package gradecal

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestPanel_LeaveOneOut(t *testing.T) {
	t.Parallel()

	ds := []Digest{
		{Name: "judge-a", Text: "short text."},
		{Name: "judge-b", Text: "this text is a little longer."},
		{Name: "candidate-c", Text: "this candidate has the longest text in the field by far."},
		{Name: "candidate-d", Text: "medium length text."},
	}
	panel := map[string]Completer{
		"judge-a": lenJudge,
		"judge-b": lenJudge,
		"judge-c": lenJudge,
	}
	judgeOrder := []string{"judge-a", "judge-b", "judge-c"}

	res := RunPanelTournament(context.Background(), panel, judgeOrder, fakeRenderer{}, "SRC", ds, 0)
	for _, edge := range res.Edges {
		if strings.HasSuffix(edge.Judge, " (fallback)") {
			continue
		}
		if edge.Judge == edge.A || edge.Judge == edge.B {
			t.Fatalf("edge %+v was self-judged", edge)
		}
	}
}

func TestPanel_Deterministic(t *testing.T) {
	t.Parallel()

	ds := []Digest{
		{Name: "judge-a", Text: "short text."},
		{Name: "judge-b", Text: "this text is a little longer."},
		{Name: "candidate-c", Text: "this candidate has the longest text in the field by far."},
		{Name: "candidate-d", Text: "medium length text."},
	}
	panel := map[string]Completer{
		"judge-a": lenJudge,
		"judge-b": lenJudge,
		"judge-c": lenJudge,
	}
	judgeOrder := []string{"judge-a", "judge-b", "judge-c"}

	first := RunPanelTournament(context.Background(), panel, judgeOrder, fakeRenderer{}, "SRC", ds, 0)
	second := RunPanelTournament(context.Background(), panel, judgeOrder, fakeRenderer{}, "SRC", ds, 0)
	if !reflect.DeepEqual(first.Ranking, second.Ranking) {
		t.Fatalf("Ranking mismatch: first %v, second %v", first.Ranking, second.Ranking)
	}
	if len(first.Edges) != len(second.Edges) {
		t.Fatalf("edge count mismatch: first %d, second %d", len(first.Edges), len(second.Edges))
	}
	for i := range first.Edges {
		if first.Edges[i].Judge != second.Edges[i].Judge {
			t.Fatalf("edge %d judge mismatch: first %q, second %q", i, first.Edges[i].Judge, second.Edges[i].Judge)
		}
	}
}

func TestPanel_JudgeCountsSumToComparisons(t *testing.T) {
	t.Parallel()

	ds := []Digest{
		{Name: "judge-a", Text: "short text."},
		{Name: "judge-b", Text: "this text is a little longer."},
		{Name: "candidate-c", Text: "this candidate has the longest text in the field by far."},
		{Name: "candidate-d", Text: "medium length text."},
	}
	panel := map[string]Completer{
		"judge-a": lenJudge,
		"judge-b": lenJudge,
		"judge-c": lenJudge,
	}
	judgeOrder := []string{"judge-a", "judge-b", "judge-c"}

	res := RunPanelTournament(context.Background(), panel, judgeOrder, fakeRenderer{}, "SRC", ds, 0)
	var count int
	for _, n := range res.JudgeCounts {
		count += n
	}
	if count != res.Comparisons {
		t.Fatalf("JudgeCounts sum = %d, want Comparisons %d", count, res.Comparisons)
	}
}

func TestPanel_FallbackWhenAllExcluded(t *testing.T) {
	t.Parallel()

	ds := []Digest{
		{Name: "judge-a", Text: "short text."},
		{Name: "judge-b", Text: "this text is a little longer."},
	}
	panel := map[string]Completer{
		"judge-a": lenJudge,
		"judge-b": lenJudge,
	}
	judgeOrder := []string{"judge-a", "judge-b"}

	res := RunPanelTournament(context.Background(), panel, judgeOrder, fakeRenderer{}, "SRC", ds, 0)
	for _, edge := range res.Edges {
		if strings.HasSuffix(edge.Judge, " (fallback)") {
			return
		}
	}
	t.Fatalf("no fallback judge found in edges: %+v", res.Edges)
}
