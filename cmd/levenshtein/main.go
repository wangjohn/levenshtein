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
	cacheRoot, _ := os.UserCacheDir()

	opts, err := parseArgs(args, options{
		source:   source,
		shared:   os.Getenv("LEVENSHTEIN_SHARED_ROOT"),
		cacheDir: filepath.Join(cacheRoot, "levenshtein", "verification-v1"),
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

	cacheDir, err := filepath.Abs(opts.cacheDir)
	if err != nil {
		return 2, err
	}
	for _, root := range []string{plan.Source, shared} {
		relative, relErr := filepath.Rel(root, cacheDir)
		if relErr == nil && filepath.IsLocal(relative) {
			return 2, fmt.Errorf("cache directory must be outside source and shared checkouts")
		}
	}

	cache := &verify.Cache{Dir: cacheDir}
	dagger := &verify.Dagger{}
	defer func() { _ = dagger.Close() }() // Session teardown does not change the reported verification result.

	report := verify.Execute(ctx, plan, shared, map[verify.ExecutorKind]verify.Executor{
		verify.ExecutorDagger: verify.CachedExecutor{Cache: cache, Executor: dagger},
		verify.ExecutorNative: verify.CachedExecutor{Cache: cache, Executor: &verify.Native{Cache: cache}},
	})

	if err := encoder.Encode(report); err != nil {
		return 2, err
	}
	if report.Status != verify.StatusPassed {
		return 1, nil
	}
	return 0, nil
}
