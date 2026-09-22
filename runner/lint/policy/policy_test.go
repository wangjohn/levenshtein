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
