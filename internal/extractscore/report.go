package extractscore

import (
	"fmt"
	"sort"
	"strings"
)

// Candidate pairs a label with its scored run.
type Candidate struct {
	Name string
	Run  RunResult
}

// RenderSummary returns a Markdown summary for one candidate: a per-chunk recall
// table plus the micro-average.
func RenderSummary(c Candidate) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", c.Name)
	fmt.Fprintf(&b, "Overall recall: %.3f (%d/%d)\n\n", c.Run.Recall(), c.Run.Covered, c.Run.Total)
	b.WriteString("| chunk | recall | covered | total | missing |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, cr := range c.Run.Chunks {
		fmt.Fprintf(&b, "| %s | %.2f | %d | %d | %s |\n",
			cr.Chunk, cr.Recall(), cr.Covered, cr.Total, strings.Join(cr.Missing, ", "))
	}
	return b.String()
}

// RenderINDEX returns a Markdown table ranking candidates by overall recall
// (descending). Ties break on candidate name (ascending) so the ranking is
// deterministic regardless of --candidates argument order.
func RenderINDEX(cands []Candidate) string {
	ranked := make([]Candidate, len(cands))
	copy(ranked, cands)
	sort.SliceStable(ranked, func(i, j int) bool {
		if ri, rj := ranked[i].Run.Recall(), ranked[j].Run.Recall(); ri != rj {
			return ri > rj
		}
		return ranked[i].Name < ranked[j].Name
	})
	var b strings.Builder
	b.WriteString("# Extraction recall ranking\n\n")
	b.WriteString("| rank | candidate | recall | covered | total |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for i, c := range ranked {
		fmt.Fprintf(&b, "| %d | %s | %.3f | %d | %d |\n",
			i+1, c.Name, c.Run.Recall(), c.Run.Covered, c.Run.Total)
	}
	return b.String()
}
