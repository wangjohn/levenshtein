package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/pflag"
	"github.com/wangjohn/levenshtein/internal/verify"
)

func main() { os.Exit(run()) }
func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	source, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	opts, err := parseArgs(os.Args[1:], options{
		source: source,
		shared: os.Getenv("LEVENSHTEIN_SHARED_ROOT"),
	}, os.Stdout)
	if errors.Is(err, pflag.ErrHelp) {
		return 0
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	shared := opts.shared

	source, err = filepath.Abs(opts.source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cfg, err := verify.Load(source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	plan, err := cfg.Plan(source, opts.name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if opts.dry {
		if err := encoder.Encode(plan); err != nil {
			return 2
		}
		return 0
	}
	if shared == "" {
		fmt.Fprintln(os.Stderr, "set --shared to the pinned Levenshtein checkout, or use its ./verify launcher")
		return 2
	}
	shared, err = filepath.Abs(shared)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	dagger := &verify.Dagger{}
	defer dagger.Close()
	report := verify.Execute(ctx, plan, shared, map[string]verify.Executor{"dagger": dagger})
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if report.Status != "passed" {
		return 1
	}
	return 0
}
