// Package lvrules is a rule module whose rules fail on purpose, so tests can
// prove a failing rule is an error and never a pass, even from the cache.
package lvrules

import (
	"errors"
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

const Namespace = "faulty"

var Renamed = map[string]string{"fine": "ok"}

func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{ok, oops, boom}
}

// ok reports every function, so a run shows that healthy rules still report.
var ok = &analysis.Analyzer{
	Name: "ok",
	Doc:  "report every function",
	URL:  "https://example.com/faulty/ok",
	Run: func(pass *analysis.Pass) (any, error) {
		for _, file := range pass.Files {
			for _, decl := range file.Decls {
				if fn, isFunc := decl.(*ast.FuncDecl); isFunc {
					pass.Reportf(fn.Pos(), "function %s", fn.Name.Name)
				}
			}
		}
		return nil, nil
	},
}

// oops returns an error, which Staticcheck would otherwise swallow.
var oops = &analysis.Analyzer{
	Name: "oops",
	Doc:  "fail with an error",
	URL:  "https://example.com/faulty/oops",
	Run: func(*analysis.Pass) (any, error) {
		return nil, errors.New("oops failed on purpose")
	},
}

// boom panics, which would otherwise kill the whole linter.
var boom = &analysis.Analyzer{
	Name: "boom",
	Doc:  "fail with a panic",
	URL:  "https://example.com/faulty/boom",
	Run: func(*analysis.Pass) (any, error) {
		panic("boom failed on purpose")
	},
}
