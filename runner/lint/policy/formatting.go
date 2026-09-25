package policy

import (
	"bytes"
	"go/format"
	"os"

	"golang.org/x/tools/go/analysis"
)

// UnformattedMessage is LV1005's finding. levenshtein-lint prints the same
// finding for the files a build leaves out, which no analyzer sees.
const UnformattedMessage = "file is not gofmt-formatted; run gofmt -w"

// Formatting removes the need for a separate gofmt step in CI. It checks the
// files each build compiles, which Staticcheck's cache keys a package on;
// levenshtein-lint checks the files a build leaves out after the run.
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
		if !Formatted(source) {
			pass.Reportf(start, UnformattedMessage)
		}
	}
	return nil, nil
}

// Formatted reports whether source is what gofmt writes. A file the formatter
// cannot parse is already a compile error elsewhere, so it counts as formatted.
func Formatted(source []byte) bool {
	formatted, err := format.Source(source)
	return err != nil || bytes.Equal(source, formatted)
}
