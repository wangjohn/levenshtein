package policy

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Fields keeps every struct field readable on a line of its own.
var Fields = &analysis.Analyzer{
	Name: "LV1003",
	Doc:  "declare each struct field on its own line, including fields that share a type",
	Run:  runFields,
}

func runFields(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		if Generated(pass.Fset, file) {
			continue
		}

		ast.Inspect(file, func(node ast.Node) bool {
			structType, ok := node.(*ast.StructType)
			if !ok || structType.Fields == nil {
				return true
			}
			for _, field := range structType.Fields.List {
				if len(field.Names) > 1 {
					pass.Reportf(field.Pos(), "declare each struct field on its own line; %s share one declaration", fieldNames(field))
				}
			}
			return true
		})
	}
	return nil, nil
}

func fieldNames(field *ast.Field) string {
	names := make([]string, 0, len(field.Names))
	for _, name := range field.Names {
		names = append(names, name.Name)
	}
	return strings.Join(names, ", ")
}
