// Package nopanic reports calls to the builtin panic in library code.
package nopanic

import (
	"go/ast"
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Analyzer reports panic calls in library code, where a caller cannot recover
// from them. Package main, test files, generated files, init functions, and
// Must functions may panic.
var Analyzer = &analysis.Analyzer{
	Name:     "nopanic",
	Doc:      "return an error from library code instead of calling panic",
	URL:      "https://pkg.go.dev/github.com/wangjohn/levenshtein/examples/rule-module/nopanic",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

func run(pass *analysis.Pass) (any, error) {
	if pass.Pkg.Name() == "main" {
		return nil, nil
	}

	inspect := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	for cursor := range inspect.Root().Preorder((*ast.CallExpr)(nil)) {
		call := cursor.Node().(*ast.CallExpr)
		if !builtinPanic(pass, call) || testFile(pass, call) || generated(cursor) {
			continue
		}

		enclosing := enclosingFunc(cursor)
		switch {
		case enclosing == nil:
			pass.Reportf(call.Pos(), "package-level code panics; return an error so callers can handle the failure")
		case !exempt(enclosing):
			pass.Reportf(call.Pos(), "%s panics; return an error so callers can handle the failure", enclosing.Name.Name)
		}
	}
	return nil, nil
}

// enclosingFunc returns the declared function a call is in, through any
// function literals, or nil for a call in package-level code.
func enclosingFunc(cursor inspector.Cursor) *ast.FuncDecl {
	for enclosing := range cursor.Enclosing((*ast.FuncDecl)(nil)) {
		return enclosing.Node().(*ast.FuncDecl)
	}
	return nil
}

// exempt reports whether a function may panic by convention: init has no
// caller to return to, and a Must function promises to panic.
func exempt(decl *ast.FuncDecl) bool {
	name := decl.Name.Name
	return (decl.Recv == nil && name == "init") || mustName(name)
}

// mustName matches Must and MustParse, but not Mustard.
func mustName(name string) bool {
	rest, ok := strings.CutPrefix(name, "Must")
	if !ok {
		return false
	}

	next, _ := utf8.DecodeRuneInString(rest)
	return rest == "" || unicode.IsUpper(next) || unicode.IsDigit(next)
}

// generated reports whether a call is in a file marked "Code generated ...
// DO NOT EDIT.", which nobody edits by hand.
func generated(cursor inspector.Cursor) bool {
	for file := range cursor.Enclosing((*ast.File)(nil)) {
		return ast.IsGenerated(file.Node().(*ast.File))
	}
	return false
}

func testFile(pass *analysis.Pass, call *ast.CallExpr) bool {
	return strings.HasSuffix(pass.Fset.Position(call.Pos()).Filename, "_test.go")
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
