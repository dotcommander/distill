package codeeval

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/dotcommander/distill/internal/ai"
)

func TestExtractCode(t *testing.T) {
	t.Parallel()
	if got := ExtractCode("pre\n```go\nX\n```\npost"); got != "X" {
		t.Errorf("fenced = %q want X", got)
	}
	if got := ExtractCode("bare code"); got != "bare code" {
		t.Errorf("bare = %q", got)
	}
}

func TestScanCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		safe bool
	}{
		{"pure", "package solution\nimport \"math\"\nvar _ = math.Pi", true},
		{"exec", "package solution\nimport \"os/exec\"\nvar _ = exec.Command", false},
		{"net", "package solution\nimport \"net/http\"\nvar _ = http.MethodGet", false},
		{"removeall", "package solution\nimport \"os\"\nfunc f() { os.RemoveAll(\"/\") }", false},
		{"unparseable", "package solution\nfunc f( {", true},
	}
	for _, c := range cases {
		if got := ScanCode(c.src); got.Safe != c.safe {
			t.Errorf("%s: Safe=%v want %v (%v)", c.name, got.Safe, c.safe, got.Reasons)
		}
	}
}

func TestLoadProblems(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	wr := func(name, body string) string {
		p := dir + "/" + name
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := wr("good.json", `{"problems":[{"id":"a","prompt":"p","signature":"func A() int","func_name":"A","cases":[{"call":"A()","want":"1"}]}]}`)
	if _, err := LoadProblems(good); err != nil {
		t.Fatalf("good rejected: %v", err)
	}
	both := wr("both.json", `{"problems":[{"id":"a","prompt":"p","signature":"s","func_name":"A","cases":[{"call":"A()","want":"1"}],"harness":"x"}]}`)
	if _, err := LoadProblems(both); err == nil {
		t.Error("expected reject: both cases+harness")
	}
	neither := wr("neither.json", `{"problems":[{"id":"a","prompt":"p","signature":"s","func_name":"A"}]}`)
	if _, err := LoadProblems(neither); err == nil {
		t.Error("expected reject: neither cases nor harness")
	}
	dup := wr("dup.json", `{"problems":[{"id":"a","prompt":"p","signature":"s","func_name":"A","cases":[{"call":"A()","want":"1"}]},{"id":"a","prompt":"p","signature":"s","func_name":"A","cases":[{"call":"A()","want":"1"}]}]}`)
	if _, err := LoadProblems(dup); err == nil {
		t.Error("expected reject: dup id")
	}
}

type fakeRenderer struct{}

func (fakeRenderer) RenderCode(signature, prompt string) string { return signature + "\n" + prompt }

type fakeCompleter struct {
	reply string
	err   error
}

func (f fakeCompleter) Complete(_ context.Context, _ string) (string, error) { return f.reply, f.err }

type recordingRunner struct {
	mu  sync.Mutex
	ran []string
}

func (r *recordingRunner) Run(_ context.Context, p Problem, _ string) ProblemOutcome {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ran = append(r.ran, p.ID)
	return ProblemOutcome{ID: p.ID, Passed: len(p.Cases), Total: len(p.Cases), Solved: true}
}

func twoProblems() ProblemSet {
	return ProblemSet{Problems: []Problem{
		{ID: "p1", Prompt: "x", Signature: "func A() int", FuncName: "A", Cases: []Case{{Call: "A()", Want: "1"}}},
		{ID: "p2", Prompt: "y", Signature: "func B() int", FuncName: "B", Cases: []Case{{Call: "B()", Want: "2"}}},
	}}
}

func TestRunModel_SafeReachesRunner(t *testing.T) {
	t.Parallel()
	rr := &recordingRunner{}
	c := fakeCompleter{reply: "```go\npackage solution\nfunc A() int { return 1 }\n```"}
	ms, err := RunModel(context.Background(), "m", c, fakeRenderer{}, rr, twoProblems(), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.ran) != 2 {
		t.Errorf("ran=%v want 2", rr.ran)
	}
	if ms.PassRate != 1.0 || ms.Solved != 2 {
		t.Errorf("PassRate=%v Solved=%d", ms.PassRate, ms.Solved)
	}
}

func TestRunModel_BlockedNeverRuns(t *testing.T) {
	t.Parallel()
	rr := &recordingRunner{}
	c := fakeCompleter{reply: "```go\npackage solution\nimport \"os/exec\"\nfunc A() int { exec.Command(\"x\"); return 1 }\n```"}
	ms, err := RunModel(context.Background(), "m", c, fakeRenderer{}, rr, twoProblems(), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.ran) != 0 {
		t.Errorf("blocked code reached runner: %v", rr.ran)
	}
	if ms.Blocked != 2 {
		t.Errorf("Blocked=%d want 2", ms.Blocked)
	}
}

func TestRunModel_EmptySkipped(t *testing.T) {
	t.Parallel()
	rr := &recordingRunner{}
	c := fakeCompleter{err: ai.ErrEmptyResponse}
	ms, err := RunModel(context.Background(), "m", c, fakeRenderer{}, rr, twoProblems(), RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rr.ran) != 0 {
		t.Errorf("skipped reached runner: %v", rr.ran)
	}
	if ms.PassRate != 0 || ms.Solved != 0 {
		t.Errorf("PassRate=%v Solved=%d", ms.PassRate, ms.Solved)
	}
}

func TestLocalRunner_TimeoutIsNotSolved(t *testing.T) {
	t.Parallel()
	p := Problem{
		ID:        "hang",
		Signature: "func A() int",
		FuncName:  "A",
		Cases:     []Case{{Call: "A()", Want: "1"}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	out := LocalRunner{}.Run(ctx, p, "func A() int { for {} }")
	if out.Solved || out.Passed != 0 || !out.CompileFail {
		t.Fatalf("timeout outcome = %+v, want unsolved compile/execution failure", out)
	}
}

func TestRankByPassRate(t *testing.T) {
	t.Parallel()
	scores := []ModelScore{
		{Model: "b", PassRate: 0.5},
		{Model: "a", PassRate: 1.0},
		{Model: "c", PassRate: 0.5, Solved: 3},
	}
	RankByPassRate(scores)
	if scores[0].Model != "a" {
		t.Errorf("rank0=%s want a", scores[0].Model)
	}
	if scores[1].Model != "c" || scores[2].Model != "b" {
		t.Errorf("tie-break wrong: %s %s", scores[1].Model, scores[2].Model)
	}
}
