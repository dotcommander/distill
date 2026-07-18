package gradecal

import (
	"context"
	"hash/fnv"
)

// runPanelTournament ranks digests using a leave-one-out judge panel: each
// comparison is ruled by one deterministic panel member that is not either
// candidate when possible. criterion selects the judging rubric: "" =
// merit/faithfulness, "comedy" = funnier bit, "publish" = publication-editor.
// source is topic/context only.
func runPanelTournament(ctx context.Context, panel map[string]Completer, judgeOrder []string, r Renderer, source string, digests []Digest, auditTriples int, criterion string) TournamentResult {
	res := TournamentResult{Records: map[string]*WL{}, JudgeCounts: map[string]int{}}
	c := &comparator{ctx: ctx, resolve: func(a, b string) (Completer, string) {
		eligible := make([]string, 0, len(judgeOrder))
		for _, name := range judgeOrder {
			if name != a && name != b {
				eligible = append(eligible, name)
			}
		}
		if len(eligible) == 0 {
			name := judgeOrder[0]
			return panel[name], name + " (fallback)"
		}
		name := eligible[stableIndex(a, b)%len(eligible)]
		return panel[name], name
	}, r: r, source: source, criterion: criterion,
		text: map[string]string{}, cache: map[string]string{}, res: &res}
	names := make([]string, len(digests))
	for i, d := range digests {
		names[i] = d.Name
		c.text[d.Name] = d.Text
	}
	res.Ranking = c.mergeSort(names)
	c.auditCycles(res.Ranking, auditTriples)
	return res
}

func stableIndex(a, b string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(pairKey(a, b)))
	return int(h.Sum32() & 0x7fffffff)
}

// RunPanelTournament judges with the default merit/faithfulness criterion.
func RunPanelTournament(ctx context.Context, panel map[string]Completer, judgeOrder []string, r Renderer, source string, digests []Digest, auditTriples int) TournamentResult {
	return runPanelTournament(ctx, panel, judgeOrder, r, source, digests, auditTriples, "")
}

// RunComedyPanelTournament judges comedy bits with the "funnier bit" criterion.
func RunComedyPanelTournament(ctx context.Context, panel map[string]Completer, judgeOrder []string, r Renderer, source string, bits []Digest, auditTriples int) TournamentResult {
	return runPanelTournament(ctx, panel, judgeOrder, r, source, bits, auditTriples, "comedy")
}

// RunPublishPanelTournament judges with the publication-editor criterion.
func RunPublishPanelTournament(ctx context.Context, panel map[string]Completer, judgeOrder []string, r Renderer, source string, digests []Digest, auditTriples int) TournamentResult {
	return runPanelTournament(ctx, panel, judgeOrder, r, source, digests, auditTriples, "publish")
}
