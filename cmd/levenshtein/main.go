package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	code, err := runCommand(ctx, os.Args[1:], os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return code
}

func runCommand(ctx context.Context, args []string, output io.Writer) (int, error) {
	source, err := os.Getwd()
	if err != nil {
		return 2, err
	}
	opts, err := parseArgs(args, options{
		source: source,
		shared: os.Getenv("LEVENSHTEIN_SHARED_ROOT"),
	}, output)
	if errors.Is(err, pflag.ErrHelp) {
		return 0, nil
	}
	if err != nil {
		return 2, err
	}
	source, err = filepath.Abs(opts.source)
	if err != nil {
		return 2, err
	}
	cfg, err := verify.Load(source)
	if err != nil {
		return 2, err
	}
	plan, err := cfg.Plan(source, opts.name)
	if err != nil {
		return 2, err
	}
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	if opts.dry {
		if err := encoder.Encode(plan); err != nil {
			return 2, err
		}
		return 0, nil
	}
	if opts.shared == "" {
		return 2, fmt.Errorf("set --shared to the pinned Levenshtein checkout, or use its ./verify launcher")
	}
	shared, err := filepath.Abs(opts.shared)
	if err != nil {
		return 2, err
	}
	dagger := &verify.Dagger{}
	defer dagger.Close()
	report := verify.Execute(ctx, plan, shared, map[string]verify.Executor{"dagger": dagger})
	if err := encoder.Encode(report); err != nil {
		return 2, err
	}
	if report.Status != "passed" {
		return 1, nil
	}
	return 0, nil
}
