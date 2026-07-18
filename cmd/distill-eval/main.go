package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/dotcommander/distill/cmd"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "distill-eval:", err)
		os.Exit(1)
	}
}

// run owns the command lifecycle so deferred teardown (e.g. signal-context
// cleanup) fires before the process exits. It returns errors instead of
// calling os.Exit, leaving the exit decision to main.
func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return cmd.ExecuteEval(ctx, os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}
