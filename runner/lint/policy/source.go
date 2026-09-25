package policy

import (
	"go/ast"
	"go/token"
)

// SourceName names the file a developer edits for one parsed file of a
// package. For a package that imports "C", the analyzers see cgo's rewrite of
// each hand-written file, which lives in the build cache and names the
// original in the //line directive under its generated header; that original
// is the source. Every other file is its own source, whatever its own //line
// directives say, so a generated copy of a Go file or a goyacc parser is
// judged as it always was.
func SourceName(fset *token.FileSet, file *ast.File) string {
	if original, ok := cgoOriginal(fset, file); ok {
		return original
	}
	return fset.File(file.FileStart).Name()
}

// Generated reports whether nobody edits the source of a parsed file; see
// isGenerated.
func Generated(fset *token.FileSet, file *ast.File) bool {
	return isGenerated(fset, file)
}
