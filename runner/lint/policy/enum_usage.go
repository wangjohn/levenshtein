package policy

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// Usage-based detection deliberately requires a finite set, not one special value.
func checkEnumUsage(pass *analysis.Pass, node ast.Node) bool {
	var subject ast.Expr
	values := map[string]bool{}
	switch n := node.(type) {
	case *ast.SwitchStmt:
		subject = n.Tag
		for _, stmt := range n.Body.List {
			for _, expr := range stmt.(*ast.CaseClause).List {
				value, ok := stringChoice(pass, expr)
				if !ok {
					return false
				}
				if value != "" {
					values[value] = true
				}
			}
		}
	case *ast.BinaryExpr:
		if n.Op != token.LOR && n.Op != token.LAND {
			return false
		}
		comparison := token.EQL
		if n.Op == token.LAND {
			comparison = token.NEQ
		}
		var collect func(ast.Expr) bool
		collect = func(expr ast.Expr) bool {
			binary, ok := ast.Unparen(expr).(*ast.BinaryExpr)
			if !ok {
				return false
			}
			if binary.Op == n.Op {
				return collect(binary.X) && collect(binary.Y)
			}
			if binary.Op != comparison {
				return false
			}
			candidate, literal := binary.X, binary.Y
			value, ok := stringChoice(pass, literal)
			if !ok {
				candidate, literal = literal, candidate
				value, ok = stringChoice(pass, literal)
			}
			if !ok {
				return false
			}
			if subject == nil {
				subject = candidate
			}
			if !sameSubject(pass, subject, candidate) {
				return false
			}
			if value != "" {
				values[value] = true
			}
			return true
		}
		if !collect(n) {
			return false
		}
	default:
		return false
	}
	if subject == nil || len(values) < 2 || !types.Identical(pass.TypesInfo.TypeOf(subject), types.Typ[types.String]) {
		return false
	}
	if !sameSubject(pass, subject, subject) {
		return false
	}
	pass.Reportf(node.Pos(), "string choice with multiple alternatives needs a defined string type and typed constants")
	return true
}

func stringChoice(pass *analysis.Pass, expr ast.Expr) (string, bool) {
	value := pass.TypesInfo.Types[expr].Value
	if value == nil || value.Kind() != constant.String {
		return "", false
	}
	text := constant.StringVal(value)
	return text, true
}

// Only stable variable/field expressions qualify; calls and indexing may change.
func sameSubject(pass *analysis.Pass, a, b ast.Expr) bool {
	switch a := ast.Unparen(a).(type) {
	case *ast.Ident:
		other, ok := ast.Unparen(b).(*ast.Ident)
		object, variable := pass.TypesInfo.ObjectOf(a).(*types.Var)
		return ok && variable && object == pass.TypesInfo.ObjectOf(other)
	case *ast.SelectorExpr:
		other, ok := ast.Unparen(b).(*ast.SelectorExpr)
		if !ok {
			return false
		}
		selection := pass.TypesInfo.Selections[a]
		if selection == nil {
			object, variable := pass.TypesInfo.ObjectOf(a.Sel).(*types.Var)
			return variable && object == pass.TypesInfo.ObjectOf(other.Sel)
		}
		otherSelection := pass.TypesInfo.Selections[other]
		return otherSelection != nil && selection.Kind() == types.FieldVal && selection.Obj() == otherSelection.Obj() && sameSubject(pass, a.X, other.X)
	}
	return false
}
