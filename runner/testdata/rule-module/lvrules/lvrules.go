// Package lvrules is the self-test's rule module: one rule that reports calls
// to a function named forbidden.
package lvrules

import (
	"go/ast"

	"golang.org/x/tools/go/analysis"
)

// Namespace prefixes the fixture's rule codes.
const Namespace = "fixture"

// Analyzers returns the fixture's rules.
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{forbidden}
}

var forbidden = &analysis.Analyzer{
	Name: "forbidden",
	Doc:  "report calls to a function named forbidden",
	URL:  "https://example.com/lvrules-fixture/forbidden",
	Run: func(pass *analysis.Pass) (any, error) {
		for _, file := range pass.Files {
			ast.Inspect(file, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if ident, named := ast.Unparen(call.Fun).(*ast.Ident); named && ident.Name == "forbidden" {
					pass.Reportf(call.Pos(), "forbidden is called")
				}
				return true
			})
		}
		return nil, nil
	},
}
