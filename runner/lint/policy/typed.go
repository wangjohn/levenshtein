// Package policy contains Levenshtein's reusable Go policy checks.
package policy

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// TypedValues keeps finite choices distinct from arbitrary text.
var TypedValues = &analysis.Analyzer{
	Name: "LV1001",
	Doc:  "use defined types for discriminator fields and enum-like string choices",
	Run:  runTypedValues,
}

func runTypedValues(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			if checkEnumUsage(pass, node) {
				if _, binary := node.(*ast.BinaryExpr); binary {
					return false
				}
			}

			// Constant declarations are where enum spellings belong.
			if decl, ok := node.(*ast.GenDecl); ok && decl.Tok == token.CONST {
				return false
			}
			if field, ok := node.(*ast.Field); ok {
				for _, name := range field.Names {
					object, ok := pass.TypesInfo.Defs[name].(*types.Var)
					if !ok || !object.IsField() || !discriminator(name.Name) {
						continue
					}
					if types.Identical(object.Type(), types.Typ[types.String]) {
						pass.Reportf(name.Pos(), "field %s needs a defined string type and typed constants", name.Name)
					}
				}
			}
			expr, ok := node.(ast.Expr)
			if !ok {
				return true
			}
			value := pass.TypesInfo.Types[expr]
			if value.Value == nil || value.Value.Kind() != constant.String || constant.StringVal(value.Value) == "" {
				return true
			}
			if !enumType(value.Type) {
				return true
			}
			switch expr := expr.(type) {
			case *ast.Ident:
				if object, ok := pass.TypesInfo.ObjectOf(expr).(*types.Const); ok && types.Identical(object.Type(), value.Type) {
					return true
				}
			case *ast.SelectorExpr:
				if object, ok := pass.TypesInfo.ObjectOf(expr.Sel).(*types.Const); ok && types.Identical(object.Type(), value.Type) {
					return true
				}
			}
			pass.Reportf(expr.Pos(), "use a typed constant for %s instead of a string literal or constant expression", value.Type)
			return false
		})
	}
	return nil, nil
}

func discriminator(name string) bool {
	switch strings.ToLower(name) {
	case "status", "state", "kind", "mode", "executor":
		return true
	default:
		return false
	}
}

func enumType(t types.Type) bool {
	if t == nil {
		return false
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok || !types.Identical(named.Underlying(), types.Typ[types.String]) || named.Obj().Pkg() == nil {
		return false
	}
	scope := named.Obj().Pkg().Scope()
	for _, name := range scope.Names() {
		if value, ok := scope.Lookup(name).(*types.Const); ok && types.Identical(value.Type(), named) {
			return true
		}
	}
	return false
}
