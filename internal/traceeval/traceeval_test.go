package traceeval

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dotcommander/distill/internal/ai"
)

func TestLastNonEmptyLine(t *testing.T) {
	t.Parallel()
	got, ok := LastNonEmptyLine("explain\n\n42  \n")
	if !ok || got != "42" {
		t.Fatalf("LastNonEmptyLine = %q, %v; want 42, true", got, ok)
	}
	if _, ok := LastNonEmptyLine("\n \n"); ok {
		t.Fatal("blank output should not parse")
	}
}

func TestLoadFixtureGeneratesAndVerifiesGold(t *testing.T) {
	t.Parallel()
	path := writeFixture(t, `{"tasks":[{"id":"a","program":"package main\nimport \"fmt\"\nfunc main(){fmt.Println(40+2)}"},{"id":"b","program":"package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"x\")}", "gold":"x"}]}`)
	fx, err := LoadFixture(context.Background(), path, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if fx.Tasks[0].Gold != "42" || fx.Tasks[1].Gold != "x" {
		t.Fatalf("gold = %#v", fx.Tasks)
	}
}

func TestLoadFixtureRejectsStaleGold(t *testing.T) {
	t.Parallel()
	path := writeFixture(t, `{"tasks":[{"id":"a","program":"package main\nimport \"fmt\"\nfunc main(){fmt.Println(40+2)}", "gold":"41"}]}`)
	if _, err := LoadFixture(context.Background(), path, 5*time.Second); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("expected stale gold error, got %v", err)
	}
}

func TestDefaultFixtureGoldIsCurrent(t *testing.T) {
	t.Parallel()
	fx, err := LoadFixture(context.Background(), "../../testdata/trace-go/tasks.json", 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(fx.Tasks) != 10 {
		t.Fatalf("tasks = %d, want 10", len(fx.Tasks))
	}
}

func TestSelectTasksFiltersByID(t *testing.T) {
	t.Parallel()
	fx := Fixture{
		Cost: map[string]float64{"m": 1},
		Tasks: []Task{
			{ID: "a"},
			{ID: "b"},
			{ID: "c"},
		},
	}
	got, err := SelectTasks(fx, []string{"c", "a"}, []string{"c"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Cost["m"] != 1 {
		t.Fatalf("cost metadata not preserved: %#v", got.Cost)
	}
	if len(got.Tasks) != 1 || got.Tasks[0].ID != "a" {
		t.Fatalf("tasks = %#v, want only a in fixture order", got.Tasks)
	}
}

func TestSelectTasksRejectsUnknownAndEmptySelection(t *testing.T) {
	t.Parallel()
	fx := Fixture{Tasks: []Task{{ID: "a"}}}
	if _, err := SelectTasks(fx, []string{"missing"}, nil); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected unknown-id error, got %v", err)
	}
	if _, err := SelectTasks(fx, []string{"a"}, []string{"a"}); err == nil || !strings.Contains(err.Error(), "selected no tasks") {
		t.Fatalf("expected empty-selection error, got %v", err)
	}
}

type fakeCompleter struct {
	reply string
	err   error
}

func (f fakeCompleter) Complete(_ context.Context, _ string) (string, error) {
	return f.reply, f.err
}

type completerFunc func(context.Context, string) (string, error)

func (f completerFunc) Complete(ctx context.Context, prompt string) (string, error) {
	return f(ctx, prompt)
}

type fakeRenderer struct{}

func (fakeRenderer) RenderTraceGo(program string) string { return program }

func TestRunModelScoresLastLine(t *testing.T) {
	t.Parallel()
	fx := Fixture{Tasks: []Task{{ID: "a", Program: "p", Gold: "42"}}}
	ms, err := RunModel(context.Background(), "m", fakeCompleter{reply: "reasoning\n42\n"}, fakeRenderer{}, fx, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ms.Accuracy != 1 || ms.Correct != 1 {
		t.Fatalf("score = %+v", ms)
	}
}

func TestRunModelEmptySkipped(t *testing.T) {
	t.Parallel()
	fx := Fixture{Tasks: []Task{{ID: "a", Program: "p", Gold: "42"}}}
	ms, err := RunModel(context.Background(), "m", fakeCompleter{err: ai.ErrEmptyResponse}, fakeRenderer{}, fx, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if ms.Skipped != 1 || ms.Correct != 0 {
		t.Fatalf("score = %+v", ms)
	}
}

func TestRunModelRecordsTaskTimeoutAndCallback(t *testing.T) {
	t.Parallel()
	fx := Fixture{Tasks: []Task{{ID: "a", Category: "slow", Program: "p", Gold: "42"}}}
	var callbacks []Prediction
	ms, err := RunModel(
		context.Background(),
		"m",
		completerFunc(func(ctx context.Context, _ string) (string, error) {
			<-ctx.Done()
			return "", ctx.Err()
		}),
		fakeRenderer{},
		fx,
		RunOptions{
			PerTask: time.Nanosecond,
			OnPrediction: func(pred Prediction) {
				callbacks = append(callbacks, pred)
			},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if ms.Skipped != 1 || ms.Correct != 0 || len(ms.Predictions) != 1 {
		t.Fatalf("score = %+v", ms)
	}
	pred := ms.Predictions[0]
	if pred.Status() != "error" || pred.Error == "" || pred.ElapsedMS < 0 {
		t.Fatalf("prediction = %+v", pred)
	}
	if len(callbacks) != 1 || callbacks[0].Status() != "error" {
		t.Fatalf("callbacks = %+v", callbacks)
	}
}

func TestRankByAccuracy(t *testing.T) {
	t.Parallel()
	scores := []ModelScore{{Model: "b", Accuracy: 0.5}, {Model: "a", Accuracy: 1}, {Model: "c", Accuracy: 0.5, Correct: 3}}
	RankByAccuracy(scores)
	if scores[0].Model != "a" || scores[1].Model != "c" || scores[2].Model != "b" {
		t.Fatalf("rank order = %#v", scores)
	}
}

func TestRenderHTMLEscapesModelAndShowsCost(t *testing.T) {
	t.Parallel()
	html := RenderHTML([]ModelScore{{
		Model:       `local<fast>`,
		Accuracy:    1,
		Correct:     2,
		N:           2,
		CostPerMTok: 0.125,
		Predictions: []Prediction{{
			ID:        `task<1>`,
			Category:  "unicode",
			Gold:      "42",
			Pred:      `local<fast>`,
			Correct:   true,
			ElapsedMS: 7,
		}},
	}})
	for _, want := range []string{
		"Go trace routing",
		"local&lt;fast&gt;",
		"100.0%",
		"2/2",
		"$/MTok",
		"$0.125",
		"Task details",
		"task&lt;1&gt;",
		"unicode",
		"ok",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("RenderHTML missing %q in %s", want, html)
		}
	}
	if strings.Contains(html, "local<fast>") {
		t.Fatalf("RenderHTML did not escape model name: %s", html)
	}
}

func writeFixture(t *testing.T, body string) string {
	t.Helper()
	path := t.TempDir() + "/fixture.json"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
