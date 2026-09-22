package policy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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

// A driver that predates Pass.ReadFile leaves it nil; LV1005 then reads the
// file itself rather than failing.
func TestFormattingReadsTheFileWithoutPassReadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "messy.go")
	if err := os.WriteFile(path, []byte("package messy\nvar  x = 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}

	var reported []analysis.Diagnostic
	pass := &analysis.Pass{Analyzer: Formatting, Fset: fset, Files: []*ast.File{file}, Report: func(d analysis.Diagnostic) { reported = append(reported, d) }}
	if _, err := Formatting.Run(pass); err != nil {
		t.Fatal(err)
	}
	if len(reported) != 1 {
		t.Fatalf("an unformatted file read without Pass.ReadFile gave %d diagnostics, want 1", len(reported))
	}
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
