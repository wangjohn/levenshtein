package community

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// Staticcheck's runner swallows an error an analyzer returns: the package
// still passes, and the run caches that pass. A panic in an analyzer kills
// the whole process instead. The guard turns both into recorded failures the
// caller reports, and keeps a failed run's results out of every later run by
// moving the Staticcheck cache to a new generation the moment anything fails.
//
// runner/lint/cmd/levenshtein-lint/guard.go keeps a copy for the core linter;
// change both together.

// guard wraps analyzers and records their failures.
type guard struct {
	mu       sync.Mutex
	wrapped  map[*analysis.Analyzer]bool
	owners   map[*analysis.Analyzer]owner
	failures map[*analysis.Analyzer]*guardFailure
	order    []*analysis.Analyzer
	onFail   func()
	failed   bool
}

// owner is how a failure names the analyzer: its code, or its own name when it
// is a dependency, and the module that brought it in.
type owner struct {
	Code   string
	Source string
}

type guardFailure struct {
	Packages []string
	Err      error
}

// newGuard returns a guard that calls onFail once, on the first failure.
func newGuard(onFail func()) *guard {
	return &guard{
		wrapped:  map[*analysis.Analyzer]bool{},
		owners:   map[*analysis.Analyzer]owner{},
		failures: map[*analysis.Analyzer]*guardFailure{},
		onFail:   onFail,
	}
}

// wrap guards an analyzer and everything it requires, in place. An analyzer
// reachable from several rules is wrapped once and keeps the first owner.
func (g *guard) wrap(analyzer *analysis.Analyzer, who owner) {
	if g.wrapped[analyzer] {
		return
	}
	g.wrapped[analyzer] = true
	g.owners[analyzer] = who

	run := analyzer.Run
	analyzer.Run = func(pass *analysis.Pass) (result any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("panic: %v", recovered)
			}
			if err != nil {
				g.record(analyzer, pass.Pkg.Path(), err)
			}
		}()
		return run(pass)
	}
	for _, required := range analyzer.Requires {
		g.wrap(required, owner{Code: required.Name, Source: who.Source})
	}
}

func (g *guard) record(analyzer *analysis.Analyzer, pkg string, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	recorded, ok := g.failures[analyzer]
	if !ok {
		recorded = &guardFailure{Err: err}
		g.failures[analyzer] = recorded
		g.order = append(g.order, analyzer)
	}
	if !slices.Contains(recorded.Packages, pkg) {
		recorded.Packages = append(recorded.Packages, pkg)
	}
	if !g.failed {
		g.failed = true
		g.onFail()
	}
}

// report lists every failure in the order the analyzers first failed.
func (g *guard) report() []Failure {
	g.mu.Lock()
	defer g.mu.Unlock()

	failures := make([]Failure, 0, len(g.order))
	for _, analyzer := range g.order {
		recorded := g.failures[analyzer]
		who := g.owners[analyzer]
		packages := slices.Sorted(slices.Values(recorded.Packages))
		failures = append(failures, Failure{Code: who.Code, Source: who.Source, Packages: packages, Error: recorded.Err.Error()})
	}
	return failures
}

// generationFile names the current cache generation inside a Staticcheck
// cache directory. Each generation is a subdirectory, gen-<n>.
const generationFile = "levenshtein-generation"

// cacheGeneration points STATICCHECK_CACHE at the current generation under
// the configured cache directory and returns a function that retires it. It
// must run before Staticcheck first reads the variable. A retired generation
// is never read again, so a run that recorded a failure cannot hand its
// cached results to a later run, even if it dies before it finishes; runs
// still using the old generation are unaffected. An explicit
// STATICCHECK_CACHE=off, or a relative path Staticcheck will refuse, is left
// alone.
func cacheGeneration() (func(), error) {
	base := os.Getenv("STATICCHECK_CACHE")
	if base == "off" || (base != "" && !filepath.IsAbs(base)) {
		return func() {}, nil
	}
	if base == "" {
		dir, err := os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("STATICCHECK_CACHE is not set and there is no user cache directory: %w", err)
		}
		base = filepath.Join(dir, "staticcheck")
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return nil, err
	}

	generation, err := readGeneration(base)
	if err != nil {
		return nil, err
	}
	if err := os.Setenv("STATICCHECK_CACHE", filepath.Join(base, "gen-"+strconv.Itoa(generation))); err != nil {
		return nil, err
	}
	prune(base, generation)
	return func() { retire(base, generation) }, nil
}

// prune removes generations older than the one before current, so failures
// do not leave whole caches behind. The previous generation is kept for runs
// that started before the last failure. A run so long that it still uses an
// older one loses its cache mid-run and fails with an error, never a pass.
func prune(base string, current int) {
	entries, err := os.ReadDir(base)
	if err != nil {
		return
	}
	for _, entry := range entries {
		number, ok := strings.CutPrefix(entry.Name(), "gen-")
		generation, err := strconv.Atoi(number)
		if ok && err == nil && entry.IsDir() && generation < current-1 {
			_ = os.RemoveAll(filepath.Join(base, entry.Name())) // Best effort: a leftover only costs disk.
		}
	}
}

func readGeneration(base string) (int, error) {
	data, err := os.ReadFile(filepath.Join(base, generationFile))
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	generation, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || generation < 0 {
		return 0, fmt.Errorf("%s holds %q, not a cache generation", filepath.Join(base, generationFile), data)
	}
	return generation, nil
}

// retire moves the cache past generation. It only ever moves forward, so two
// runs that fail at once leave a generation neither of them wrote to. A
// failure to write is reported and otherwise ignored: the run is already an
// error, and the next failing run retires the generation again.
func retire(base string, generation int) {
	current, err := readGeneration(base)
	if err == nil && current > generation {
		return
	}

	temporary, err := os.CreateTemp(base, generationFile+"-*")
	if err == nil {
		_, err = temporary.WriteString(strconv.Itoa(generation+1) + "\n")
		if closeErr := temporary.Close(); err == nil {
			err = closeErr
		}
		if err == nil {
			err = os.Rename(temporary.Name(), filepath.Join(base, generationFile))
		}
		if err != nil {
			_ = os.Remove(temporary.Name()) // Best effort; the error below is what matters.
		}
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "retiring the Staticcheck cache generation after a rule failure: %v\n", err)
	}
}
