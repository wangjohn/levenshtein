// Package lvrules is a rule module whose rules fail or misbehave on purpose,
// so tests can prove a failing rule is an error and never a pass, even from
// the cache, and that a rule's findings reach the report however it files
// them.
package lvrules

import (
	"errors"
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

const Namespace = "faulty"

var Renamed = map[string]string{"fine": "ok"}

func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{ok, styled, oops, boom}
}

// ok reports every function, so a run shows that healthy rules still report.
var ok = &analysis.Analyzer{
	Name: "ok",
	Doc:  "report every function",
	URL:  "https://example.com/faulty/ok",
	Run:  functions(""),
}

// styled reports every function under a category of its own, as analyzers
// such as nilness do. Staticcheck drops a finding whose category is not a
// registered check.
var styled = &analysis.Analyzer{
	Name: "styled",
	Doc:  "report every function under the style category",
	URL:  "https://example.com/faulty/styled",
	Run:  functions("style"),
}

func functions(category string) func(*analysis.Pass) (any, error) {
	return func(pass *analysis.Pass) (any, error) {
		for _, file := range pass.Files {
			for _, decl := range file.Decls {
				if fn, isFunc := decl.(*ast.FuncDecl); isFunc {
					pass.Report(analysis.Diagnostic{Pos: fn.Pos(), Category: category, Message: "function " + fn.Name.Name})
				}
			}
		}
		return nil, nil
	}
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
