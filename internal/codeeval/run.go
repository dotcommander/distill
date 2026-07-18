package codeeval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dotcommander/distill/internal/ai"
)

// Completer is the generation backend (satisfied by *ai.Client).
type Completer interface {
	Complete(ctx context.Context, prompt string) (string, error)
}

// Renderer builds the code-writing prompt (satisfied by *prompts.Set).
type Renderer interface {
	RenderCode(signature, prompt string) string
}

// RunOptions bounds a run. PerProblem is each problem's own timeout.
type RunOptions struct {
	Concurrency int // reserved; problems already run concurrently per model
	PerProblem  time.Duration
}

// ProblemOutcome is the deterministic result for one problem.
type ProblemOutcome struct {
	ID          string
	Passed      int  // test cases passed
	Total       int  // test cases total
	Solved      bool // all passed, compiled
	CompileFail bool // code did not compile
	Blocked     bool // static scan rejected it (never executed)
	Skipped     bool // empty/failed model response
}

// ModelScore mirrors labelscore.ModelScore's shape: PassRate is the headline,
// ElapsedMS the latency axis, CostPerMTok shown only when >0.
type ModelScore struct {
	Model        string
	PassRate     float64 // sum(Passed)/sum(Total)
	Solved       int     // problems fully solved
	CompileFails int
	Blocked      int
	N            int // problems attempted
	CostPerMTok  float64
	ElapsedMS    int64
	Outcomes     []ProblemOutcome
}

// Runner executes model code for ONE problem. The default LocalRunner uses
// temp-dir + offline `go test` + timeout (NON-sandboxed). A container Runner can
// replace it later without touching generation/scoring.
type Runner interface {
	Run(ctx context.Context, p Problem, code string) ProblemOutcome
}

var fenceRe = regexp.MustCompile("(?s)```(?:go)?\\s*\\n(.*?)```")
var pkgRe = regexp.MustCompile(`(?m)^\s*package\s+\w+`)

// ExtractCode strips a single ```go ... ``` fence; if none, returns trimmed raw.
func ExtractCode(raw string) string {
	if m := fenceRe.FindStringSubmatch(raw); m != nil {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(raw)
}

// forcePackage normalizes the solution to `package solution` (models sometimes
// emit package main or omit the clause).
func forcePackage(src string) string {
	if pkgRe.MatchString(src) {
		return pkgRe.ReplaceAllString(src, "package solution")
	}
	return "package solution\n" + src
}

// genTest builds the deterministic test file. For Cases, each case runs in a
// panic-recovering closure so one panic fails only that case; a failed case
// prints CASE_FAIL (counted by the runner). For Harness, the literal body is
// used verbatim (it supplies its own imports).
func genTest(p Problem) string {
	if strings.TrimSpace(p.Harness) != "" {
		return "package solution\n\n" + p.Harness + "\n"
	}
	var b strings.Builder
	b.WriteString("package solution\n\nimport (\n\t\"reflect\"\n\t\"testing\"\n)\n\n")
	b.WriteString("func TestCases(t *testing.T) {\n")
	b.WriteString("\trun := func(i int, fn func() bool) {\n")
	b.WriteString("\t\tdefer func() {\n\t\t\tif r := recover(); r != nil {\n\t\t\t\tt.Errorf(\"CASE_FAIL %d panic %v\", i, r)\n\t\t\t}\n\t\t}()\n")
	b.WriteString("\t\tif !fn() {\n\t\t\tt.Errorf(\"CASE_FAIL %d\", i)\n\t\t}\n\t}\n")
	for i, c := range p.Cases {
		fmt.Fprintf(&b, "\trun(%d, func() bool { return reflect.DeepEqual(%s, %s) })\n", i, c.Call, c.Want)
	}
	b.WriteString("}\n")
	return b.String()
}

// minimalEnv is the child env for `go test`: offline, no parent-env leakage
// beyond what the toolchain needs.
func minimalEnv() []string {
	env := []string{"GOPROXY=off", "GOSUMDB=off", "GOFLAGS=-mod=mod", "CGO_ENABLED=0"}
	for _, k := range []string{"PATH", "HOME", "GOCACHE", "GOPATH", "GOROOT"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// LocalRunner compiles + tests model code in a throwaway temp dir. Defense-in-
// depth (offline, minimal env, timeout), NOT a sandbox.
type LocalRunner struct{}

func (LocalRunner) Run(ctx context.Context, p Problem, code string) ProblemOutcome {
	out := ProblemOutcome{ID: p.ID, Total: len(p.Cases)}
	dir, err := os.MkdirTemp("", "codeeval-*")
	if err != nil {
		out.CompileFail = true
		return out
	}
	defer func() { _ = os.RemoveAll(dir) }()
	write := func(name, content string) error {
		return os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600)
	}
	if write("go.mod", "module solution\n\ngo 1.26\n") != nil ||
		write("solution.go", forcePackage(code)) != nil ||
		write("solution_test.go", genTest(p)) != nil {
		out.CompileFail = true
		return out
	}
	cmd := exec.CommandContext(ctx, "go", "test", "-run", "TestCases", "-count=1", ".")
	cmd.Dir = dir
	cmd.Env = minimalEnv()
	output, runErr := cmd.CombinedOutput()
	s := string(output)
	if strings.Contains(s, "[build failed]") || strings.Contains(s, "cannot find package") {
		out.CompileFail = true
		return out
	}
	fails := strings.Count(s, "CASE_FAIL")
	if runErr != nil && fails == 0 {
		out.CompileFail = true
		return out
	}
	out.Passed = out.Total - fails
	if out.Passed < 0 {
		out.Passed = 0
	}
	out.Solved = fails == 0 && out.Total > 0
	return out
}

// RunModel has one model solve every problem CONCURRENTLY (per-problem timeout,
// plain WaitGroup so one failure doesn't cancel siblings). Each solution is
// extracted, statically scanned (unsafe -> Blocked, never executed), then run.
func RunModel(ctx context.Context, model string, c Completer, r Renderer, runner Runner, ps ProblemSet, opts RunOptions) (ModelScore, error) {
	start := time.Now()
	outcomes := make([]ProblemOutcome, len(ps.Problems))
	var wg sync.WaitGroup
	for i, p := range ps.Problems {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pctx := ctx
			if opts.PerProblem > 0 {
				var cancel context.CancelFunc
				pctx, cancel = context.WithTimeout(ctx, opts.PerProblem)
				defer cancel()
			}
			prompt := r.RenderCode(p.Signature, p.Prompt)
			raw, cerr := c.Complete(pctx, prompt)
			if cerr != nil {
				// Empty or transport/timeout: recorded miss, not fatal to the run.
				_ = errors.Is(cerr, ai.ErrEmptyResponse)
				outcomes[i] = ProblemOutcome{ID: p.ID, Total: len(p.Cases), Skipped: true}
				return
			}
			code := ExtractCode(raw)
			if scan := ScanCode(code); !scan.Safe {
				outcomes[i] = ProblemOutcome{ID: p.ID, Total: len(p.Cases), Blocked: true}
				return
			}
			outcomes[i] = runner.Run(pctx, p, code)
		}()
	}
	wg.Wait()

	ms := ModelScore{Model: model, N: len(ps.Problems), Outcomes: outcomes, ElapsedMS: time.Since(start).Milliseconds()}
	var passed, total int
	for _, o := range outcomes {
		passed += o.Passed
		total += o.Total
		if o.Solved {
			ms.Solved++
		}
		if o.CompileFail {
			ms.CompileFails++
		}
		if o.Blocked {
			ms.Blocked++
		}
	}
	if total > 0 {
		ms.PassRate = float64(passed) / float64(total)
	}
	if ps.Cost != nil {
		ms.CostPerMTok = ps.Cost[model]
	}
	return ms, nil
}

// RankByPassRate sorts model scores best-first (pass-rate, then solved, then name).
func RankByPassRate(scores []ModelScore) {
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].PassRate != scores[j].PassRate {
			return scores[i].PassRate > scores[j].PassRate
		}
		if scores[i].Solved != scores[j].Solved {
			return scores[i].Solved > scores[j].Solved
		}
		return scores[i].Model < scores[j].Model
	})
}
