package community

import (
	"fmt"
	"sync"

	"golang.org/x/tools/go/analysis"
)

// Staticcheck's runner swallows an error an analyzer returns: the package
// still passes, and the run caches that pass. A panic in an analyzer kills the
// whole process instead. The guard catches both and hands the first failure
// to its caller, which stops the process at once. Staticcheck writes a
// package's results to its cache only after every analyzer on that package
// has finished, so a package whose analysis failed is never cached, wherever
// the cache lives: a directory, or a GOCACHEPROG program.
//
// This is a copy of runner/lint/cmd/levenshtein-lint/guard.go, which guards
// the core linter; change both together.

// guard wraps analyzers and stops the run on the first failure.
type guard struct {
	mu      sync.Mutex
	wrapped map[*analysis.Analyzer]bool
	stop    func(failure)
}

// owner is how a failure names the analyzer: its code, or its own name when it
// is a dependency, and the module that brought it in.
type owner struct {
	Code   string
	Source string
}

// failure is one analyzer that returned an error or panicked.
type failure struct {
	Code    string
	Source  string
	Package string
	Error   string
}

// newGuard returns a guard that calls stop with the first failure. stop is
// expected to end the process; the guard holds its lock while stop runs, so a
// second failure never reaches it.
func newGuard(stop func(failure)) *guard {
	return &guard{wrapped: map[*analysis.Analyzer]bool{}, stop: stop}
}

// wrap guards an analyzer and everything it requires, in place. An analyzer
// reachable from several rules is wrapped once and keeps the first owner.
func (g *guard) wrap(analyzer *analysis.Analyzer, who owner) {
	if g.wrapped[analyzer] {
		return
	}
	g.wrapped[analyzer] = true

	run := analyzer.Run
	analyzer.Run = func(pass *analysis.Pass) (result any, err error) {
		defer func() {
			if recovered := recover(); recovered != nil {
				err = fmt.Errorf("panic: %v", recovered)
			}
			if err != nil {
				g.fail(failure{Code: who.Code, Source: who.Source, Package: pass.Pkg.Path(), Error: err.Error()})
			}
		}()
		return run(pass)
	}
	for _, required := range analyzer.Requires {
		g.wrap(required, owner{Code: required.Name, Source: who.Source})
	}
}

func (g *guard) fail(f failure) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.stop(f)
}
