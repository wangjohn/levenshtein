package policy

import (
	"go/ast"
	"testing"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestTypedValues(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), TypedValues, "typed")
}

func TestRecords(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Records, "records", "consumer")
}

func TestFields(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Fields, "fields")
}

func TestSpacing(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Spacing, "spacing")
}

func TestFormatting(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Formatting, "formatting")
}

func TestAssertions(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Assertions, "assertions")
}

// functions reports every function declaration, so Adapt's filter alone
// decides which files report.
func functions() *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: "functions",
		Doc:  "report every function declaration",
		Run: func(pass *analysis.Pass) (any, error) {
			for _, file := range pass.Files {
				for _, decl := range file.Decls {
					if fn, ok := decl.(*ast.FuncDecl); ok {
						pass.Reportf(fn.Pos(), "function %s", fn.Name.Name)
					}
				}
			}
			return nil, nil
		},
	}
}

// A package that imports "C" is analyzed through cgo's rewrite of each file,
// which carries a generated header and lives in the build cache. Each file is
// judged by its original: the hand-written file reports, while the generated
// file and cgo's own additions, such as _cgo_gotypes.go, stay silent.
func TestCgoFiles(t *testing.T) {
	t.Setenv("CGO_ENABLED", "1")
	analysistest.Run(t, analysistest.TestData(), Adapt(functions())[0], "cgo")
	analysistest.Run(t, analysistest.TestData(), Fields, "cgofields")
	analysistest.Run(t, analysistest.TestData(), Formatting, "cgoformat")
}

// A //line directive outside cgo's rewrite does not change which file is
// generated: a generated copy that maps back to a hand-written Go file stays
// silent, and a hand-written file that maps to a template still reports.
func TestLineDirectives(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Adapt(functions())[0], "linedirective")
}
