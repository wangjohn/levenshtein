// Package nopanic reports calls to the builtin panic in library code.
package nopanic

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer reports panic calls outside package main, init functions, and
// Must-prefixed constructors, where a caller cannot recover from them.
var Analyzer = &analysis.Analyzer{
	Name:     "nopanic",
	Doc:      "return an error from library code instead of calling panic",
	URL:      "https://github.com/wangjohn/levenshtein/tree/main/examples/rule-module#nopanic",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	if pass.Pkg.Name() == "main" {
		return nil, nil
	}

	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	for cursor := range inspect.Root().Preorder((*ast.FuncDecl)(nil)) {
		decl := cursor.Node().(*ast.FuncDecl)
		if decl.Body == nil || exempt(decl) {
			continue
		}

		ast.Inspect(decl.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if ok && builtinPanic(pass, call) {
				pass.Reportf(call.Pos(), "%s panics; return an error so callers can handle the failure", decl.Name.Name)
			}
			return true
		})
	}
	return nil, nil
}

// exempt reports whether a function is allowed to panic by convention: init
// has no caller to return to, and a Must function promises to panic.
func exempt(decl *ast.FuncDecl) bool {
	name := decl.Name.Name
	return (decl.Recv == nil && name == "init") || strings.HasPrefix(name, "Must")
}

// builtinPanic uses type information, so a local function that happens to be
// named panic is not reported.
func builtinPanic(pass *analysis.Pass, call *ast.CallExpr) bool {
	ident, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}

	builtin, ok := pass.TypesInfo.Uses[ident].(*types.Builtin)
	return ok && builtin.Name() == "panic"
}
