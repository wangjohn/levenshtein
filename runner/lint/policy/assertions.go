package policy

import (
	"go/ast"
	"go/types"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/tools/go/analysis"
)

// Assertions reports tests that pass no matter what the code under test does.
var Assertions = &analysis.Analyzer{
	Name: "LV1006",
	Doc:  "give every test a way to fail, and do not skip a test unconditionally",
	Run:  runAssertions,
}

// quietMethods are testing.TB methods that can neither fail the test nor hand
// the test value to code that might. Every other use of a test value counts as
// a way to fail: an Error or Fatal call, a helper or assertion library that
// receives it, or a subtest body the analysis cannot see.
var quietMethods = map[string]bool{
	"ArtifactDir": true,
	"Attr":        true,
	"Chdir":       true,
	"Cleanup":     true,
	"Context":     true,
	"Deadline":    true,
	"Helper":      true,
	"Log":         true,
	"Logf":        true,
	"Name":        true,
	"Output":      true,
	"Parallel":    true,
	"Setenv":      true,
	"Skip":        true,
	"SkipNow":     true,
	"Skipf":       true,
	"Skipped":     true,
	"TempDir":     true,
}

var skipMethods = map[string]bool{
	"Skip":    true,
	"SkipNow": true,
	"Skipf":   true,
}

func runAssertions(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		if ast.IsGenerated(file) || !strings.HasSuffix(pass.Fset.File(file.FileStart).Name(), "_test.go") {
			continue
		}

		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !testFunction(pass, fn) {
				continue
			}
			if skip := unconditionalSkip(pass, fn.Body); skip != nil {
				pass.Reportf(skip.Pos(), "%s always skips; delete the test or skip under a condition", fn.Name.Name)
				continue
			}
			if !canFail(pass, fn.Body) {
				pass.Reportf(fn.Name.Pos(), "%s cannot fail; assert on the behavior it exercises", fn.Name.Name)
			}
		}
	}
	return nil, nil
}

// testFunction matches what go test runs as a test: a top-level TestXxx whose
// name does not continue in lowercase, taking one *testing.T.
func testFunction(pass *analysis.Pass, fn *ast.FuncDecl) bool {
	if fn.Recv != nil || fn.Type.TypeParams != nil {
		return false
	}
	rest, ok := strings.CutPrefix(fn.Name.Name, "Test")
	if !ok {
		return false
	}
	if next, _ := utf8.DecodeRuneInString(rest); rest != "" && unicode.IsLower(next) {
		return false
	}
	params := fn.Type.Params.List
	if len(params) != 1 || len(params[0].Names) > 1 {
		return false
	}
	pointer, ok := pass.TypesInfo.TypeOf(params[0].Type).(*types.Pointer)
	return ok && testingNamed(pointer.Elem(), "T")
}

// testingTB reports whether typ is *testing.T or testing.TB, the values whose
// methods report a test failure.
func testingTB(typ types.Type) bool {
	if pointer, ok := typ.(*types.Pointer); ok {
		return testingNamed(pointer.Elem(), "T")
	}
	return testingNamed(typ, "TB")
}

func testingNamed(typ types.Type, name string) bool {
	named, ok := typ.(*types.Named)
	if !ok || named.Obj().Pkg() == nil {
		return false
	}
	return named.Obj().Pkg().Path() == "testing" && named.Obj().Name() == name
}

// unconditionalSkip returns a skip call made directly in the test body, which
// runs on every invocation. A skip inside a branch is a condition and is allowed.
func unconditionalSkip(pass *analysis.Pass, body *ast.BlockStmt) *ast.CallExpr {
	for _, stmt := range body.List {
		expr, ok := stmt.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := expr.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		method, ok := call.Fun.(*ast.SelectorExpr)
		if ok && skipMethods[method.Sel.Name] && testValue(pass, method.X) {
			return call
		}
	}
	return nil
}

// canFail reports whether anything in body can fail the test: a test value
// used other than as the receiver of a quiet method, or an explicit panic.
// Nested function literals are included, so subtest bodies and cleanup
// callbacks count.
func canFail(pass *analysis.Pass, body *ast.BlockStmt) bool {
	quiet := map[*ast.Ident]bool{}
	fails := false
	ast.Inspect(body, func(node ast.Node) bool {
		if fails {
			return false
		}
		switch n := node.(type) {
		case *ast.SelectorExpr:
			receiver, ok := ast.Unparen(n.X).(*ast.Ident)
			if ok && quietMethods[n.Sel.Name] && testValue(pass, receiver) {
				quiet[receiver] = true
			}
		case *ast.CallExpr:
			if receiver := visibleSubtest(pass, n); receiver != nil {
				quiet[receiver] = true
			}
			if builtinCall(pass, n, "panic") {
				fails = true
			}
		case *ast.Ident:
			if !quiet[n] && testValue(pass, n) {
				fails = true
			}
		}
		return true
	})
	return fails
}

// visibleSubtest returns the receiver of t.Run(name, func(t *testing.T) {...}).
// The literal body is inspected in place, so Run itself is quiet. Run with a
// named function hands control to code outside this test, which counts as a
// way to fail, so it returns nil.
func visibleSubtest(pass *analysis.Pass, call *ast.CallExpr) *ast.Ident {
	method, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || method.Sel.Name != "Run" || len(call.Args) != 2 {
		return nil
	}
	receiver, ok := ast.Unparen(method.X).(*ast.Ident)
	if !ok || !testValue(pass, receiver) {
		return nil
	}
	if _, literal := ast.Unparen(call.Args[1]).(*ast.FuncLit); !literal {
		return nil
	}
	return receiver
}

// testValue reports whether expr uses a variable of type *testing.T or
// testing.TB. Declarations, such as a subtest literal's parameter, are not uses.
func testValue(pass *analysis.Pass, expr ast.Expr) bool {
	ident, ok := ast.Unparen(expr).(*ast.Ident)
	if !ok {
		return false
	}
	variable, ok := pass.TypesInfo.Uses[ident].(*types.Var)
	return ok && testingTB(variable.Type())
}

func builtinCall(pass *analysis.Pass, call *ast.CallExpr, name string) bool {
	ident, ok := ast.Unparen(call.Fun).(*ast.Ident)
	if !ok {
		return false
	}
	builtin, ok := pass.TypesInfo.Uses[ident].(*types.Builtin)
	return ok && builtin.Name() == name
}
