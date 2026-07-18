package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/dotcommander/distill/internal/codexresets"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "codex-resets:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	return codexresets.Run(ctx, os.Stdout, codexresets.Options{})
}
