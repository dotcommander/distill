package extractscore

import (
	"fmt"
	"sort"
	"strings"
)

// RenderStructuredSummary returns a Markdown field report for one structured
// extraction candidate.
func RenderStructuredSummary(r StructuredResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Structured extraction: %s\n\n", r.Name)
	fmt.Fprintf(&b, "- Structural pass: %t\n", r.StructuralPass)
	fmt.Fprintf(&b, "- Field score: %.3f\n", r.FieldScore)
	fmt.Fprintf(&b, "- Item score: %.3f\n", r.ItemScore)
	fmt.Fprintf(&b, "- Pass rate: %.3f (%d/%d)\n", r.PassRate, r.Passed, r.Total)
	if len(r.StructuralErrors) > 0 {
		b.WriteString("\n## Structural errors\n\n")
		for _, err := range r.StructuralErrors {
			fmt.Fprintf(&b, "- %s\n", err)
		}
	}
	b.WriteString("\n## Fields\n\n")
	b.WriteString("| path | metric | score | pass | weight | array P/R/F1 | reason |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for _, f := range r.Fields {
		array := ""
		if f.Matched+f.Missed+f.Spurious > 0 {
			array = fmt.Sprintf("%.3f/%.3f/%.3f", f.Precision, f.Recall, f.F1)
		}
		fmt.Fprintf(&b, "| %s | %s | %.3f | %t | %d | %s | %s |\n",
			f.Path, f.Metric, f.Score, f.Passed, f.Weight, array, strings.ReplaceAll(f.Reason, "|", "\\|"))
	}
	return b.String()
}

// RenderStructuredINDEX returns a Markdown ranking by structural pass, item
// score, field score, then candidate name.
func RenderStructuredINDEX(results []StructuredResult) string {
	ranked := make([]StructuredResult, len(results))
	copy(ranked, results)
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].StructuralPass != ranked[j].StructuralPass {
			return ranked[i].StructuralPass
		}
		if ranked[i].ItemScore != ranked[j].ItemScore {
			return ranked[i].ItemScore > ranked[j].ItemScore
		}
		if ranked[i].FieldScore != ranked[j].FieldScore {
			return ranked[i].FieldScore > ranked[j].FieldScore
		}
		return ranked[i].Name < ranked[j].Name
	})
	var b strings.Builder
	b.WriteString("# Structured extraction ranking\n\n")
	b.WriteString("| rank | candidate | structural | item score | field score | pass rate | fields |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	for i, r := range ranked {
		fmt.Fprintf(&b, "| %d | %s | %t | %.3f | %.3f | %.3f | %d/%d |\n",
			i+1, r.Name, r.StructuralPass, r.ItemScore, r.FieldScore, r.PassRate, r.Passed, r.Total)
	}
	return b.String()
}
