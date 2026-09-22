package policy

import (
	"bytes"
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
		if Generated(pass.Fset, file) {
			continue
		}

		// For a package that imports "C", the parsed file is cgo's rewrite in
		// the build cache; the file to check and report is the original.
		// Pass.ReadFile only permits the rewrite's name, so the original is
		// read directly.
		name := SourceName(pass.Fset, file)
		readSource, start := read, file.FileStart
		if name != pass.Fset.File(file.FileStart).Name() {
			readSource, start = os.ReadFile, file.Package
		}
		source, err := readSource(name)
		if err != nil {
			return nil, err
		}
		formatted, err := format.Source(source)
		if err != nil {
			// A file the formatter cannot parse is already a compile error elsewhere.
			continue
		}
		if !bytes.Equal(source, formatted) {
			pass.Reportf(start, "file is not gofmt-formatted; run gofmt -w")
		}
	}
	return nil, nil
}
