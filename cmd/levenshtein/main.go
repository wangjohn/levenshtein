package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
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

	code, err := runCommand(ctx, os.Args[1:], console{in: os.Stdin, out: os.Stdout})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	return code
}

// console is where a command reads a saved report from and writes its report
// to. An error that ends the command is returned, not written.
type console struct {
	in  io.Reader
	out io.Writer
}

func runCommand(ctx context.Context, args []string, streams console) (int, error) {
	source, err := os.Getwd()
	if err != nil {
		return 2, err
	}
	cacheRoot, _ := os.UserCacheDir()

	opts, err := parseArgs(args, options{
		source:   source,
		shared:   os.Getenv("LEVENSHTEIN_SHARED_ROOT"),
		cacheDir: filepath.Join(cacheRoot, "levenshtein", "verification-v1"),
	}, streams.out)
	if errors.Is(err, pflag.ErrHelp) {
		return 0, nil
	}
	if err != nil {
		return 2, err
	}
	if opts.render != "" {
		return renderSaved(opts, streams)
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

	if opts.dry {
		encoder := json.NewEncoder(streams.out)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(plan); err != nil {
			return 2, err
		}
		return 0, nil
	}

	if opts.shared == "" {
		return 2, fmt.Errorf("set --shared to the pinned Levenshtein checkout, or use its ./verify launcher")
	}
	// Resolve the shared checkout the same way Plan resolves the source, so one
	// checkout has one spelling in fingerprints and memoized snapshots.
	shared, err := resolveExisting(opts.shared)
	if err != nil {
		return 2, fmt.Errorf("--shared %q: %w", opts.shared, err)
	}
	if _, err := os.Stat(shared); err != nil {
		return 2, fmt.Errorf("--shared %q: %w", opts.shared, err)
	}

	// The cache directory may not exist yet; resolve what does exist so the
	// containment check below compares like with like.
	cacheDir, err := resolveExisting(opts.cacheDir)
	if err != nil {
		return 2, fmt.Errorf("--cache-dir %q: %w", opts.cacheDir, err)
	}
	for _, root := range []string{plan.Source, shared} {
		relative, relErr := filepath.Rel(root, cacheDir)
		if relErr == nil && filepath.IsLocal(relative) {
			return 2, fmt.Errorf("cache directory must be outside source and shared checkouts")
		}
	}

	cache := &verify.Cache{Dir: cacheDir}
	// The file stat memo is a hint the next run revalidates, so failing to
	// persist it changes nothing this run reported.
	defer func() { _ = cache.Flush() }()
	dagger := &verify.Dagger{}
	defer func() { _ = dagger.Close() }() // Session teardown does not change the reported verification result.

	report := verify.Execute(ctx, plan, shared, map[verify.ExecutorKind]verify.Executor{
		verify.ExecutorDagger: verify.CachedExecutor{Cache: cache, Executor: dagger},
		verify.ExecutorNative: verify.CachedExecutor{Cache: cache, Executor: &verify.Native{Cache: cache}},
	}, opts.jobs)

	// Hints apply to the finished report, after the cache has stored whatever
	// it stores, so they never reach a cached result.
	report = verify.WithHints(report)
	if err := verify.Render(streams.out, report, opts.format, verify.RenderOptions{PathPrefix: opts.pathPrefix}); err != nil {
		return 2, err
	}
	if report.Status != verify.StatusPassed {
		return 1, nil
	}
	return 0, nil
}

// renderSaved writes a report an earlier run saved as JSON in another format.
// It runs nothing, so it exits 0 once the report is written, whatever the
// report's own status.
func renderSaved(opts options, streams console) (int, error) {
	input := streams.in
	if opts.render != "-" {
		file, err := os.Open(opts.render)
		if err != nil {
			return 2, err
		}
		defer func() { _ = file.Close() }() // Read-only file cleanup.
		input = file
	}

	var report verify.Report
	if err := json.NewDecoder(input).Decode(&report); err != nil {
		return 2, fmt.Errorf("--render %q: %w", opts.render, err)
	}
	if report.Version != 1 {
		return 2, fmt.Errorf("--render %q: not a version 1 Levenshtein report", opts.render)
	}
	if err := verify.Render(streams.out, verify.WithHints(report), opts.format, verify.RenderOptions{PathPrefix: opts.pathPrefix}); err != nil {
		return 2, err
	}
	return 0, nil
}

// resolveExisting makes path absolute and resolves symlinks in its longest
// existing prefix, keeping any trailing components that do not exist yet.
func resolveExisting(path string) (string, error) {
	path, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}

	var missing []string
	for current := path; ; current = filepath.Dir(current) {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			return filepath.Join(append([]string{resolved}, missing...)...), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		if filepath.Dir(current) == current {
			return "", err
		}
		missing = append([]string{filepath.Base(current)}, missing...)
	}
}
