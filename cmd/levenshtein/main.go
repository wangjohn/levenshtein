package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/wangjohn/levenshtein/internal/verify"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
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
	shared := os.Getenv("LEVENSHTEIN_SHARED_ROOT")
	cacheRoot, _ := os.UserCacheDir()
	cacheDir := filepath.Join(cacheRoot, "levenshtein", "verification-v1")
	name := "branch"
	named := false
	dry := false
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			fmt.Println("Usage: verify [RUN] [--source DIRECTORY] [--shared DIRECTORY] [--cache-dir DIRECTORY] [--dry-run]")
			return 0
		case arg == "--dry-run":
			dry = true
		case arg == "--source" || arg == "--shared" || arg == "--cache-dir":
			if i+1 == len(args) {
				fmt.Fprintln(os.Stderr, "missing value for", arg)
				return 2
			}
			i++
			if arg == "--source" {
				source = args[i]
			} else if arg == "--cache-dir" {
				cacheDir = args[i]
			} else {
				shared = args[i]
			}
		case strings.HasPrefix(arg, "--source="):
			source = strings.TrimPrefix(arg, "--source=")
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintln(os.Stderr, "unknown option", arg)
			return 2
		default:
			if named {
				fmt.Fprintln(os.Stderr, "unexpected argument", arg)
				return 2
			}
			name = arg
			named = true
		}
	}
	if source == "" {
		fmt.Fprintln(os.Stderr, "source directory cannot be empty")
		return 2
	}
	source, err = filepath.Abs(source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cfg, err := verify.Load(source)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	plan, err := cfg.Plan(source, name)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if dry {
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
	cacheDir, err = filepath.Abs(cacheDir)
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
	report := verify.Execute(ctx, plan, shared, map[string]verify.Executor{
		"dagger": verify.CachedExecutor{Cache: cache, Executor: verify.Dagger{}},
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
