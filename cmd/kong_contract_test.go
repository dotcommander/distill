package cmd

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestKongRootsHelpAndRejectCompletion(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func([]string, *bytes.Buffer, *bytes.Buffer) error
	}{
		{"distill", func(a []string, o, e *bytes.Buffer) error {
			return execute(context.Background(), a, strings.NewReader(""), o, e)
		}},
		{"distill-eval", func(a []string, o, e *bytes.Buffer) error {
			return executeEval(context.Background(), a, strings.NewReader(""), o, e)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if err := tc.run(nil, &out, &errOut); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), "Usage:") {
				t.Fatalf("missing help: %q", out.String())
			}
			if err := tc.run([]string{"completion"}, &out, &errOut); err == nil {
				t.Fatal("completion unexpectedly accepted")
			}
		})
	}
}

func TestMovedBreadcrumbsPreserveExactErrors(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{{[]string{"eval", "judge", "--old"}, "this command moved to the distill-eval binary — run: distill-eval eval"}, {[]string{"models", "x"}, "this command moved to the distill-eval binary — run: distill-eval models"}, {[]string{"digest", "grade", "x"}, "this command moved to the distill-eval binary — run: distill-eval grade"}} {
		var out, errOut bytes.Buffer
		err := execute(context.Background(), tc.args, strings.NewReader(""), &out, &errOut)
		if err == nil || err.Error() != tc.want {
			t.Fatalf("%v: got %v want %q", tc.args, err, tc.want)
		}
	}
}

func TestExecuteReturnsParseErrors(t *testing.T) {
	var out, errOut bytes.Buffer
	if err := execute(context.Background(), []string{"not-a-command"}, strings.NewReader(""), &out, &errOut); err == nil {
		t.Fatal("expected error")
	}
}

type contextProbeCommand struct{}

func (*contextProbeCommand) Run(ctx context.Context) error { return ctx.Err() }

func TestParserBindsCallerContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	err := parseAndRun(ctx, "probe", "", &struct {
		Check contextProbeCommand `cmd:""`
	}{}, []string{"check"}, strings.NewReader(""), &out, &errOut)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context canceled", err)
	}
}

var errProbe = errors.New("probe failure")

type errorProbeCommand struct{}

func (*errorProbeCommand) Run() error { return errProbe }

func TestParserPropagatesReturnedError(t *testing.T) {
	var out, errOut bytes.Buffer
	err := parseAndRun(context.Background(), "probe", "", &struct {
		Check errorProbeCommand `cmd:""`
	}{}, []string{"check"}, strings.NewReader(""), &out, &errOut)
	if !errors.Is(err, errProbe) {
		t.Fatalf("got %v, want probe failure", err)
	}
}

func TestExplicitFlagPresence(t *testing.T) {
	for _, args := range [][]string{{"grade", "panel", "--judges=x"}, {"models", "comedy", "--judges", "x"}} {
		if !presentFlags(args)["judges"] {
			t.Fatalf("judges not detected in %v", args)
		}
	}
}
