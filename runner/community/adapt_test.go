package community

import (
	"go/ast"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

// functions reports every function declaration under category, so the
// linter's own handling alone decides which findings reach Staticcheck and
// under what code.
func functions(category string) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "functions",
		Doc:  "report every function declaration",
		URL:  "https://example.com/rules/functions",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				for _, decl := range file.Decls {
					if fn, ok := decl.(*ast.FuncDecl); ok {
						pass.Report(analysis.Diagnostic{Pos: fn.Pos(), Category: category, Message: "function " + fn.Name.Name})
					}
				}
			}
			return nil, nil
		},
	}
}

// registered returns analyzer the way the linter hands it to Staticcheck.
func registered(t *testing.T, analyzer *analysis.Analyzer) *analysis.Analyzer {
	t.Helper()
	module := Module{Path: errsPath, Version: "v1.4.0", Namespace: "errs", Analyzers: []*analysis.Analyzer{analyzer}}
	resolved, err := resolve([]Module{module}, Config{Modules: []ModuleConfig{errsConfig("errs_*")}})
	if err != nil {
		t.Fatal(err)
	}

	resolved.register(func(f failure) { t.Errorf("unexpected failure %+v", f) })
	return analyzer
}

// Staticcheck keeps a finding only when its category names a registered
// check, so a rule that categorizes its findings would report nothing.
func TestACategorizedRuleReportsUnderItsCode(t *testing.T) {
	analyzer := registered(t, functions("style"))

	results := analysistest.Run(t, analysistest.TestData(), analyzer, "categorized")

	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			if diagnostic.Category != "" {
				t.Errorf("%q reached Staticcheck under category %q, which it drops", diagnostic.Message, diagnostic.Category)
			}
		}
	}
}

// Community rules skip the same files as the core linter's rules: this runs
// on the core linter's own fixtures for runner/lint/policy's Adapt.
func TestGeneratedFilesStaySilentAsForCoreRules(t *testing.T) {
	core, err := filepath.Abs(filepath.Join("..", "lint", "policy", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CGO_ENABLED", "1")

	analysistest.Run(t, core, registered(t, functions("")), "cgo")
	analysistest.Run(t, core, registered(t, functions("")), "linedirective")
}
