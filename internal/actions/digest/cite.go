package digest

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dotcommander/distill/internal/extractscore"
)

var citeGroupRe = regexp.MustCompile(`\[[Ff]\d+(?:\s*,\s*[Ff]?\d+)*\]`)

// CitationResult reports whether numbered facts survived into a cited draft.
// Total is the number of extracted fact units; Covered is the number of unique
// fact IDs cited at least once in the article.
type CitationResult struct {
	Covered    int   `json:"covered"`
	Total      int   `json:"total"`
	MissingIDs []int `json:"missing_ids,omitempty"`
}

func (r CitationResult) Ratio() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Covered) / float64(r.Total)
}

func computeCitations(units []factUnit, article string) CitationResult {
	valid := make(map[int]struct{}, len(units))
	for _, u := range units {
		valid[u.id] = struct{}{}
	}
	covered := make(map[int]struct{}, len(units))
	for _, group := range citeGroupRe.FindAllString(article, -1) {
		for _, id := range parseCitationIDs(group) {
			if _, ok := valid[id]; ok {
				covered[id] = struct{}{}
			}
		}
	}
	missing := make([]int, 0, len(units)-len(covered))
	for _, u := range units {
		if _, ok := covered[u.id]; !ok {
			missing = append(missing, u.id)
		}
	}
	return CitationResult{Covered: len(covered), Total: len(units), MissingIDs: missing}
}

func parseCitationIDs(group string) []int {
	trimmed := strings.Trim(group, "[]")
	parts := strings.Split(trimmed, ",")
	ids := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "F")
		part = strings.TrimPrefix(part, "f")
		id, err := strconv.Atoi(part)
		if err == nil {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids
}

func stripCiteMarkers(s string) string {
	out := citeGroupRe.ReplaceAllString(s, "")
	replacements := []struct {
		old string
		new string
	}{
		{"  ", " "},
		{" .", "."},
		{" ,", ","},
		{" ;", ";"},
		{" :", ":"},
		{" !", "!"},
		{" ?", "?"},
		{" )", ")"},
		{"( ", "("},
	}
	for {
		next := out
		for _, repl := range replacements {
			next = strings.ReplaceAll(next, repl.old, repl.new)
		}
		if next == out {
			break
		}
		out = next
	}
	lines := strings.Split(out, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// citedSentencesForPrecision returns every sentence from the cited article
// with citation-marker groups stripped. No sentence is skipped — even a
// fully-cited article yields one stripped sentence per original sentence.
func citedSentencesForPrecision(cited string) []string {
	all := extractscore.SplitSentences(cited)
	out := make([]string, 0, len(all))
	for _, sentence := range all {
		stripped := stripCiteMarkers(sentence)
		if stripped != "" {
			out = append(out, stripped)
		}
	}
	return out
}
