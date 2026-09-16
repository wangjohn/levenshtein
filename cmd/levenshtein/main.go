package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
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
	cacheRoot, _ := os.UserCacheDir()
	opts, err := parseArgs(os.Args[1:], options{
		source:   source,
		shared:   os.Getenv("LEVENSHTEIN_SHARED_ROOT"),
		cacheDir: filepath.Join(cacheRoot, "levenshtein", "verification-v1"),
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
	cacheDir, err := filepath.Abs(opts.cacheDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	for _, root := range []string{plan.Source, shared} {
		relative, relErr := filepath.Rel(root, cacheDir)
		if relErr == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			fmt.Fprintln(os.Stderr, "cache directory must be outside source and shared checkouts")
			return 2
		}
	}
	cache := &verify.Cache{Dir: cacheDir}
	dagger := &verify.Dagger{}
	defer dagger.Close()
	report := verify.Execute(ctx, plan, shared, map[string]verify.Executor{
		"dagger": verify.CachedExecutor{Cache: cache, Executor: dagger},
		"native": verify.CachedExecutor{Cache: cache, Executor: &verify.Native{Cache: cache}},
	})

	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if report.Status != "passed" {
		return 1
	}
	return 0
}
