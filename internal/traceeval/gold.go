package traceeval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// LastNonEmptyLine returns the final line containing non-space text. The second
// return value is false when output contains no non-empty line.
func LastNonEmptyLine(s string) (string, bool) {
	for end := len(s); end > 0; {
		start := strings.LastIndexByte(s[:end], '\n') + 1
		line := strings.TrimRight(s[start:end], " \t\r")
		if strings.TrimSpace(line) == "" {
			end = start - 1
			continue
		}
		return line, true
	}
	return "", false
}

// RunProgram executes one trusted fixture program and returns its final
// non-empty stdout line. This is the gold oracle; model output is never run.
func RunProgram(ctx context.Context, t Task, timeout time.Duration) (string, error) {
	runCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	dir, err := os.MkdirTemp("", "traceeval-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if werr := os.WriteFile(filepath.Join(dir, "main.go"), []byte(t.Program), 0o600); werr != nil {
		return "", werr
	}
	cmd := exec.CommandContext(runCtx, "go", "run", "main.go")
	cmd.Dir = dir
	cmd.Env = minimalEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("go run failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	line, ok := LastNonEmptyLine(string(out))
	if !ok {
		return "", errors.New("go run produced no non-empty stdout line")
	}
	return line, nil
}

func minimalEnv() []string {
	env := []string{"GOPROXY=off", "GOSUMDB=off", "GOFLAGS=-mod=mod", "CGO_ENABLED=0"}
	for _, k := range []string{"PATH", "HOME", "GOCACHE", "GOPATH", "GOROOT"} {
		if v := os.Getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}
