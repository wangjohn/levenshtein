package policy

import (
	"bytes"
	"go/ast"
	"go/format"
	"os"

	"golang.org/x/tools/go/analysis"
)

// Formatting removes the need for a separate gofmt step in CI.
var Formatting = &analysis.Analyzer{
	Name: "LV1005",
	Doc:  "keep every Go file formatted the way gofmt writes it",
	Run:  runFormatting,
}

func runFormatting(pass *analysis.Pass) (any, error) {
	read := pass.ReadFile
	if read == nil {
		read = os.ReadFile
	}

	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			continue
		}

		name := pass.Fset.File(file.FileStart).Name()
		source, err := read(name)
		if err != nil {
			return nil, err
		}
		formatted, err := format.Source(source)
		if err != nil {
			// A file the formatter cannot parse is already a compile error elsewhere.
			continue
		}
		if !bytes.Equal(source, formatted) {
			pass.Reportf(file.FileStart, "file is not gofmt-formatted; run gofmt -w")
		}
	}
	return nil, nil
}
