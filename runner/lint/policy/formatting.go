package policy

import (
	"bytes"
	"go/ast"
	"go/build"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

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
		if !formatted(source) {
			pass.Reportf(start, "file is not gofmt-formatted; run gofmt -w")
		}
	}

	for _, name := range ignoredFiles(pass) {
		readIgnored := os.ReadFile
		if slices.Contains(pass.IgnoredFiles, name) {
			readIgnored = read
		}
		source, err := readIgnored(name)
		if err != nil {
			return nil, err
		}
		header, err := parser.ParseFile(token.NewFileSet(), name, source, parser.PackageClauseOnly|parser.ParseComments)
		if err != nil || ast.IsGenerated(header) || formatted(source) {
			continue
		}

		// An ignored file is not parsed into the package, so it joins the file
		// set only to give the diagnostic a position.
		ignored := pass.Fset.AddFile(name, -1, len(source))
		ignored.SetLinesForContent(source)
		pass.Reportf(ignored.Pos(0), "file is not gofmt-formatted; run gofmt -w")
	}
	return nil, nil
}

// formatted reports whether source is what gofmt writes. A file the formatter
// cannot parse is already a compile error elsewhere, so it counts as formatted.
func formatted(source []byte) bool {
	formatted, err := format.Source(source)
	return err != nil || bytes.Equal(source, formatted)
}

// ignoredFiles lists the Go files in the package's directory that the build
// leaves out, for another platform or behind a build tag such as integration
// or ignore. They are not type-checked, but gofmt checks them, so LV1005 does
// too. A driver may list them in Pass.IgnoredFiles; Staticcheck's runner does
// not, so the directory is read with go/build under the default build context.
// The directory is shared by the package's plain build and the builds with its
// tests, so only one build lists them: the plain build, or a build with tests
// when the directory has nothing else to build.
func ignoredFiles(pass *analysis.Pass) []string {
	var names []string
	for _, name := range pass.IgnoredFiles {
		if strings.HasSuffix(name, ".go") {
			names = append(names, name)
		}
	}
	if len(pass.Files) == 0 {
		return names
	}

	compiled := map[string]bool{}
	tests := false
	for _, file := range pass.Files {
		name := SourceName(pass.Fset, file)
		compiled[name] = true
		tests = tests || strings.HasSuffix(name, "_test.go")
	}
	dir := filepath.Dir(SourceName(pass.Fset, pass.Files[0]))
	// A directory with files of several packages, or none the build keeps,
	// still lists what it ignores.
	listed, _ := build.ImportDir(dir, 0)
	if listed == nil {
		return names
	}
	if tests && len(listed.GoFiles)+len(listed.CgoFiles) > 0 {
		return nil
	}
	for _, base := range listed.IgnoredGoFiles {
		name := filepath.Join(dir, base)
		if !compiled[name] && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}
