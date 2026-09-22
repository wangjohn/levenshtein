package policy

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
)

// Spacing keeps top-level declarations visually separated.
var Spacing = &analysis.Analyzer{
	Name: "LV1004",
	Doc:  "separate top-level declarations with a blank line",
	Run:  runSpacing,
}

func runSpacing(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			continue
		}

		for i := 1; i < len(file.Decls); i++ {
			previous, current := file.Decls[i-1], file.Decls[i]
			if importDecl(previous) {
				// gofmt already forces a blank line after the import block.
				continue
			}
			// Count physical lines: a //line directive renumbers what follows it.
			start := declStart(current)
			if physicalLine(pass, start)-physicalLine(pass, previous.End()) >= 2 {
				continue
			}
			pass.Reportf(start, "separate top-level declarations with a blank line")
		}
	}
	return nil, nil
}

func physicalLine(pass *analysis.Pass, pos token.Pos) int {
	return pass.Fset.PositionFor(pos, false).Line
}

func importDecl(decl ast.Decl) bool {
	gen, ok := decl.(*ast.GenDecl)
	return ok && gen.Tok == token.IMPORT
}

// A declaration begins at its doc comment, which shares the declaration's spacing.
func declStart(decl ast.Decl) token.Pos {
	switch n := decl.(type) {
	case *ast.GenDecl:
		if n.Doc != nil {
			return n.Doc.Pos()
		}
	case *ast.FuncDecl:
		if n.Doc != nil {
			return n.Doc.Pos()
		}
	}
	return decl.Pos()
}
