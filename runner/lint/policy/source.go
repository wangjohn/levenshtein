package policy

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
)

// SourceName names the file a developer edits for one parsed file of a
// package. For a package that imports "C", the analyzers see cgo's rewrite of
// each hand-written file, which lives in the build cache and maps back to the
// original through a //line directive before its package clause; that
// original is the source. Every other file is its own source. As in
// Staticcheck's report positions, a mapping counts only when it names a Go
// file, so a goyacc parser stays its own source rather than its grammar.
func SourceName(fset *token.FileSet, file *ast.File) string {
	mapped := fset.Position(file.Package).Filename
	if filepath.Ext(mapped) == ".go" {
		return mapped
	}
	return fset.File(file.FileStart).Name()
}

// Generated reports whether nobody edits the source of a parsed file. cgo
// marks its rewrite of a hand-written file as generated, so a rewritten file
// is judged by the header of the original it maps to. cgo's own additions,
// such as _cgo_gotypes.go and the header-less _cgo_import.go, map nowhere and
// carry the build cache's extension-less names, or a _cgo_ prefix, which the
// go command never compiles from a package directory.
func Generated(fset *token.FileSet, file *ast.File) bool {
	compiled := fset.File(file.FileStart).Name()
	source := SourceName(fset, file)
	if source == compiled {
		return ast.IsGenerated(file) || filepath.Ext(source) != ".go" || strings.HasPrefix(filepath.Base(source), "_cgo_")
	}

	header, err := parser.ParseFile(token.NewFileSet(), source, nil, parser.PackageClauseOnly|parser.ParseComments)
	if err != nil {
		// An original that cannot be read falls back to the rewrite's own header.
		return ast.IsGenerated(file)
	}
	return ast.IsGenerated(header)
}
