// These are copies of runner/community/guard_test.go; change both together.

package main

import (
	"errors"
	"go/types"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis"
)

func passFor(analyzer *analysis.Analyzer, pkg string) *analysis.Pass {
	return &analysis.Pass{Analyzer: analyzer, Pkg: types.NewPackage(pkg, filepath.Base(pkg))}
}

func TestGuardStopsOnErrorsAndPanicsAcrossTheRequiresGraph(t *testing.T) {
	dependency := &analysis.Analyzer{Name: "helper", Run: func(*analysis.Pass) (any, error) { panic("index out of range") }}
	rule := &analysis.Analyzer{Name: "errcheck", Requires: []*analysis.Analyzer{dependency}, Run: func(*analysis.Pass) (any, error) {
		return nil, errors.New("no type information")
	}}
	var stopped []failure
	g := newGuard(func(f failure) { stopped = append(stopped, f) })

	g.wrap(rule, owner{Code: "errcheck"})
	_, ruleErr := rule.Run(passFor(rule, "example.com/a"))
	_, dependencyErr := dependency.Run(passFor(dependency, "example.com/b"))

	if ruleErr == nil || dependencyErr == nil {
		t.Fatalf("a failure still reaches Staticcheck as an error: %v, %v", ruleErr, dependencyErr)
	}
	want := []failure{
		{Code: "errcheck", Package: "example.com/a", Error: "no type information"},
		{Code: "helper", Package: "example.com/b", Error: "panic: index out of range"},
	}
	if !slices.Equal(stopped, want) {
		t.Errorf("stopped with %+v, want %+v", stopped, want)
	}
}

func TestGuardWrapsAnAnalyzerOnce(t *testing.T) {
	calls := 0
	shared := &analysis.Analyzer{Name: "inspect", Run: func(*analysis.Pass) (any, error) { calls++; return nil, nil }}
	g := newGuard(func(f failure) { t.Errorf("unexpected failure %+v", f) })

	g.wrap(shared, owner{Code: "inspect"})
	g.wrap(shared, owner{Code: "inspect"})
	_, err := shared.Run(passFor(shared, "example.com/a"))

	if err != nil || calls != 1 {
		t.Errorf("a doubly wrapped analyzer ran %d times: %v", calls, err)
	}
}
