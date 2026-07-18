package extractscore

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
)

// StructuredResult is the deterministic score for one JSON extraction against a
// schema-backed gold file. Structural failures make the aggregate scores zero,
// but field outcomes are still returned when enough JSON could be decoded.
type StructuredResult struct {
	Name             string         `json:"name"`
	StructuralPass   bool           `json:"structural_pass"`
	StructuralErrors []string       `json:"structural_errors,omitempty"`
	FieldScore       float64        `json:"field_score"`
	ItemScore        float64        `json:"item_score"`
	PassRate         float64        `json:"pass_rate"`
	Passed           int            `json:"passed"`
	Total            int            `json:"total"`
	Fields           []FieldOutcome `json:"fields"`
}

// FieldOutcome records one evaluated schema path.
type FieldOutcome struct {
	Path      string          `json:"path"`
	Metric    string          `json:"metric"`
	Score     float64         `json:"score"`
	Passed    bool            `json:"passed"`
	Weight    int             `json:"weight"`
	Gold      json.RawMessage `json:"gold,omitempty"`
	Predicted json.RawMessage `json:"predicted,omitempty"`
	Reason    string          `json:"reason,omitempty"`
	Matched   int             `json:"matched,omitempty"`
	Missed    int             `json:"missed,omitempty"`
	Spurious  int             `json:"spurious,omitempty"`
	Precision float64         `json:"precision,omitempty"`
	Recall    float64         `json:"recall,omitempty"`
	F1        float64         `json:"f1,omitempty"`
}

type schemaNode struct {
	Type                 string
	Properties           map[string]schemaNode
	Required             map[string]bool
	AdditionalProperties *bool
	Items                *schemaNode
	Format               string
	Metric               string
	Tolerance            float64
	HasTolerance         bool
}

// ScoreStructuredFiles loads schema, gold JSON, and prediction JSON files and
// scores the prediction with ScoreStructured.
func ScoreStructuredFiles(name, schemaPath, goldPath, predictionPath string) (StructuredResult, error) {
	schemaData, err := os.ReadFile(schemaPath)
	if err != nil {
		return StructuredResult{}, err
	}
	goldData, err := os.ReadFile(goldPath)
	if err != nil {
		return StructuredResult{}, err
	}
	predData, err := os.ReadFile(predictionPath)
	if err != nil {
		return StructuredResult{}, err
	}
	return ScoreStructured(name, schemaData, goldData, predData)
}

// ScoreStructured scores one JSON extraction using a compact JSON Schema subset:
// object properties, required fields, additionalProperties=false, scalar types,
// arrays, and per-field evaluation_config.
func ScoreStructured(name string, schemaData, goldData, predictionData []byte) (StructuredResult, error) {
	var schemaRaw any
	if err := json.Unmarshal(schemaData, &schemaRaw); err != nil {
		return StructuredResult{}, fmt.Errorf("structured: parsing schema: %w", err)
	}
	schema, err := parseSchema(schemaRaw)
	if err != nil {
		return StructuredResult{}, err
	}
	var gold any
	if err := json.Unmarshal(goldData, &gold); err != nil {
		return StructuredResult{}, fmt.Errorf("structured: parsing gold: %w", err)
	}
	var pred any
	res := StructuredResult{Name: name, StructuralPass: true}
	if err := json.Unmarshal(predictionData, &pred); err != nil {
		res.StructuralPass = false
		res.StructuralErrors = append(res.StructuralErrors, "prediction is invalid JSON: "+err.Error())
		pred = nil
	}
	res.StructuralErrors = append(res.StructuralErrors, validateValue("$", schema, pred)...)
	if len(res.StructuralErrors) > 0 {
		res.StructuralPass = false
	}
	res.Fields = evaluateFields("$", schema, gold, pred)
	res.Total = len(res.Fields)
	for i := range res.Fields {
		if res.Fields[i].Passed {
			res.Passed++
		}
	}
	if res.Total > 0 {
		sum := 0.0
		weighted := 0.0
		weights := 0
		for _, f := range res.Fields {
			sum += f.Score
			w := f.Weight
			if w <= 0 {
				w = 1
			}
			weighted += f.Score * float64(w)
			weights += w
		}
		res.FieldScore = sum / float64(res.Total)
		res.ItemScore = weighted / float64(weights)
		res.PassRate = float64(res.Passed) / float64(res.Total)
	}
	if !res.StructuralPass {
		res.FieldScore = 0
		res.ItemScore = 0
		res.PassRate = 0
	}
	return res, nil
}

func parseSchema(raw any) (schemaNode, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return schemaNode{}, errors.New("structured: schema node must be an object")
	}
	n := schemaNode{Properties: map[string]schemaNode{}, Required: map[string]bool{}}
	if typ, ok := m["type"].(string); ok {
		n.Type = typ
	}
	if format, ok := m["format"].(string); ok {
		n.Format = format
	}
	if add, ok := m["additionalProperties"].(bool); ok {
		n.AdditionalProperties = &add
	}
	if req, ok := m["required"].([]any); ok {
		for _, v := range req {
			if s, ok := v.(string); ok {
				n.Required[s] = true
			}
		}
	}
	if props, ok := m["properties"].(map[string]any); ok {
		for k, v := range props {
			child, err := parseSchema(v)
			if err != nil {
				return schemaNode{}, fmt.Errorf("structured: property %s: %w", k, err)
			}
			n.Properties[k] = child
		}
	}
	if itemRaw, ok := m["items"]; ok {
		child, err := parseSchema(itemRaw)
		if err != nil {
			return schemaNode{}, fmt.Errorf("structured: items: %w", err)
		}
		n.Items = &child
	}
	n.Metric, n.Tolerance, n.HasTolerance = parseEvaluationConfig(m["evaluation_config"])
	return n, nil
}

func parseEvaluationConfig(raw any) (metric string, tolerance float64, hasTolerance bool) {
	switch v := raw.(type) {
	case string:
		return v, 0, false
	case map[string]any:
		if metrics, ok := v["metrics"].([]any); ok && len(metrics) > 0 {
			if first, ok := metrics[0].(map[string]any); ok {
				if id, ok := first["metric_id"].(string); ok {
					metric = id
				}
				if params, ok := first["params"].(map[string]any); ok {
					if t, ok := numberValue(params["tolerance"]); ok {
						tolerance = t
						hasTolerance = true
					}
				}
			}
		}
	}
	return metric, tolerance, hasTolerance
}

// validateObject checks an object value against an object schema node: required
// fields present, each present field valid, and (when additionalProperties is
// false) no unexpected fields.
func validateObject(p string, n schemaNode, obj map[string]any) []string {
	var errs []string
	keys := sortedSchemaKeys(n.Properties)
	for _, k := range keys {
		if _, ok := obj[k]; !ok && n.Required[k] {
			errs = append(errs, path.Join(p, k)+": missing required field")
			continue
		}
		if val, ok := obj[k]; ok {
			errs = append(errs, validateValue(path.Join(p, k), n.Properties[k], val)...)
		}
	}
	if n.AdditionalProperties != nil && !*n.AdditionalProperties {
		for k := range obj {
			if _, ok := n.Properties[k]; !ok {
				errs = append(errs, path.Join(p, k)+": unexpected field")
			}
		}
	}
	return errs
}

func validateValue(p string, n schemaNode, v any) []string {
	var errs []string
	if v == nil {
		return []string{p + ": missing value"}
	}
	switch n.Type {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return []string{p + ": expected object"}
		}
		errs = append(errs, validateObject(p, n, obj)...)
	case "array":
		arr, ok := v.([]any)
		if !ok {
			return []string{p + ": expected array"}
		}
		if n.Items != nil {
			for i, item := range arr {
				errs = append(errs, validateValue(p+"["+strconv.Itoa(i)+"]", *n.Items, item)...)
			}
		}
	case "string":
		if _, ok := v.(string); !ok {
			return []string{p + ": expected string"}
		}
	case "number":
		if _, ok := numberValue(v); !ok {
			return []string{p + ": expected number"}
		}
	case "integer":
		num, ok := numberValue(v)
		if !ok || math.Trunc(num) != num {
			return []string{p + ": expected integer"}
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return []string{p + ": expected boolean"}
		}
	}
	return errs
}

func evaluateFields(p string, n schemaNode, gold, pred any) []FieldOutcome {
	switch n.Type {
	case "object":
		var out []FieldOutcome
		goldObj, _ := gold.(map[string]any)
		predObj, _ := pred.(map[string]any)
		for _, k := range sortedSchemaKeys(n.Properties) {
			out = append(out, evaluateFields(path.Join(p, k), n.Properties[k], goldObj[k], predObj[k])...)
		}
		return out
	default:
		return []FieldOutcome{evaluateLeaf(p, n, gold, pred)}
	}
}

func evaluateLeaf(p string, n schemaNode, gold, pred any) FieldOutcome {
	metric := metricFor(n)
	out := FieldOutcome{Path: p, Metric: metric, Weight: 1, Gold: rawJSON(gold), Predicted: rawJSON(pred)}
	if pred == nil {
		out.Reason = "missing predicted value"
		return out
	}
	switch n.Type {
	case "array":
		out.Score, out.Matched, out.Missed, out.Spurious = scoreArray(gold, pred)
		out.Precision = ratio(out.Matched, out.Matched+out.Spurious)
		out.Recall = ratio(out.Matched, out.Matched+out.Missed)
		out.F1 = harmonic(out.Precision, out.Recall)
		out.Passed = out.Score == 1
		out.Weight = out.Matched + out.Missed
		if out.Weight == 0 {
			out.Weight = 1
		}
	case "string":
		out.Score = scoreString(metric, stringValue(gold), stringValue(pred))
		out.Passed = out.Score == 1
	case "number", "integer":
		out.Score = scoreNumber(metric, n, gold, pred)
		out.Passed = out.Score == 1
	case "boolean":
		out.Score = boolScore(gold, pred)
		out.Passed = out.Score == 1
	default:
		if canonicalJSON(gold) == canonicalJSON(pred) {
			out.Score = 1
			out.Passed = true
		}
	}
	return out
}

func metricFor(n schemaNode) string {
	if n.Metric != "" {
		if n.Format == "uri" && n.Metric == "string_exact" {
			return "string_url"
		}
		return n.Metric
	}
	switch n.Type {
	case "string":
		if n.Format == "uri" {
			return "string_url"
		}
		return "string_exact"
	case "number":
		return "number_tolerance"
	case "integer":
		return "integer_exact"
	case "boolean":
		return "boolean_exact"
	case "array":
		return "array_exact"
	default:
		return "exact"
	}
}

func scoreString(metric, gold, pred string) float64 {
	switch metric {
	case "string_case_insensitive":
		if strings.EqualFold(gold, pred) {
			return 1
		}
	case "string_fuzzy":
		return levenshteinSimilarity(strings.ToLower(gold), strings.ToLower(pred))
	case "string_url":
		if normalizeURL(gold) == normalizeURL(pred) {
			return 1
		}
	default:
		if gold == pred {
			return 1
		}
	}
	return 0
}

func scoreNumber(metric string, n schemaNode, gold, pred any) float64 {
	g, okG := numberValue(gold)
	p, okP := numberValue(pred)
	if !okG || !okP {
		return 0
	}
	switch metric {
	case "number_exact", "integer_exact":
		if g == p {
			return 1
		}
	default:
		tolerance := n.Tolerance
		if !n.HasTolerance {
			tolerance = 0
		}
		if math.Abs(g-p) <= tolerance {
			return 1
		}
	}
	return 0
}

func scoreArray(gold, pred any) (score float64, matched int, missed int, spurious int) {
	goldArr, okG := gold.([]any)
	predArr, okP := pred.([]any)
	if !okG || !okP {
		return 0, 0, len(goldArr), len(predArr)
	}
	remaining := make(map[string]int, len(predArr))
	for _, v := range predArr {
		remaining[canonicalJSON(v)]++
	}
	for _, v := range goldArr {
		key := canonicalJSON(v)
		if remaining[key] > 0 {
			matched++
			remaining[key]--
		} else {
			missed++
		}
	}
	for _, n := range remaining {
		spurious += n
	}
	return ratio(matched, matched+missed), matched, missed, spurious
}

func boolScore(gold, pred any) float64 {
	if g, ok := gold.(bool); ok {
		if p, ok := pred.(bool); ok && g == p {
			return 1
		}
	}
	return 0
}

func numberValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func ratio(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func harmonic(p, r float64) float64 {
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

func sortedSchemaKeys(m map[string]schemaNode) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func rawJSON(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

func canonicalJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func normalizeURL(s string) string {
	u, err := url.Parse(strings.TrimSpace(strings.ToLower(s)))
	if err == nil && u.Host != "" {
		host := strings.TrimPrefix(u.Host, "www.")
		return host + strings.TrimRight(u.Path, "/")
	}
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "www.")
	return strings.TrimRight(strings.ToLower(s), "/")
}

func levenshteinSimilarity(a, b string) float64 {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 && len(br) == 0 {
		return 1
	}
	dist := levenshteinDistance(ar, br)
	maxLen := max(len(ar), len(br))
	return 1 - float64(dist)/float64(maxLen)
}

func levenshteinDistance(a, b []rune) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			cur[j] = min(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}
