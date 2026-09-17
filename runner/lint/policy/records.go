package policy

import (
	"go/ast"
	"go/token"
	"go/types"
	"maps"

	"golang.org/x/tools/go/analysis"
)

// Records detects staged construction before a new local struct is first used.
var Records = &analysis.Analyzer{
	Name: "LV1002",
	Doc:  "construct new local structs with literals instead of assigning their fields before first use",
	Run:  runRecords,
}

func runRecords(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			var body *ast.BlockStmt
			switch n := node.(type) {
			case *ast.FuncDecl:
				body = n.Body
			case *ast.FuncLit:
				body = n.Body
			}
			if body != nil {
				checkConstruction(pass, body)
			}
			return true
		})
	}
	return nil, nil
}

func structType(t types.Type) bool {
	if t == nil {
		return false
	}
	if pointer, ok := types.Unalias(t).(*types.Pointer); ok {
		t = pointer.Elem()
	}
	_, ok := t.Underlying().(*types.Struct)
	return ok
}

func newStruct(pass *analysis.Pass, expr ast.Expr) bool {
	switch n := ast.Unparen(expr).(type) {
	case *ast.CompositeLit:
		return structType(pass.TypesInfo.TypeOf(n))
	case *ast.UnaryExpr:
		return n.Op == token.AND && newStruct(pass, n.X)
	case *ast.CallExpr:
		id, ok := n.Fun.(*ast.Ident)
		if !ok {
			return false
		}
		builtin, ok := pass.TypesInfo.ObjectOf(id).(*types.Builtin)
		return ok && builtin.Name() == "new" && structType(pass.TypesInfo.TypeOf(n))
	}
	return false
}

func fieldRoot(pass *analysis.Pass, expr ast.Expr) *types.Var {
	switch n := ast.Unparen(expr).(type) {
	case *ast.Ident:
		object, _ := pass.TypesInfo.ObjectOf(n).(*types.Var)
		return object
	case *ast.SelectorExpr:
		if selection := pass.TypesInfo.Selections[n]; selection != nil && selection.Kind() == types.FieldVal {
			return fieldRoot(pass, n.X)
		}
	case *ast.StarExpr:
		return fieldRoot(pass, n.X)
	}
	return nil
}

func checkConstruction(pass *analysis.Pass, body *ast.BlockStmt) {
	pending := map[*types.Var]token.Pos{}
	reported := map[token.Pos]bool{}
	// Any read, alias, escape, or compound update ends the construction window.
	consume := func(node ast.Node) {
		if node == nil {
			return
		}
		ast.Inspect(node, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok {
				if object, ok := pass.TypesInfo.ObjectOf(id).(*types.Var); ok {
					delete(pending, object)
				}
			}
			return true
		})
	}
	var visit func(ast.Stmt)
	visit = func(stmt ast.Stmt) {
		switch n := stmt.(type) {
		case *ast.BlockStmt:
			for _, child := range n.List {
				visit(child)
			}
		case *ast.IfStmt:
			if n.Init != nil {
				visit(n.Init)
			}
			consume(n.Cond)
			before := maps.Clone(pending)
			visit(n.Body)
			pending = maps.Clone(before)
			if n.Else != nil {
				visit(n.Else)
			}
			pending = before
			consume(n)
		case *ast.DeclStmt:
			consume(n)
			decl, ok := n.Decl.(*ast.GenDecl)
			if !ok || decl.Tok != token.VAR {
				return
			}
			for _, spec := range decl.Specs {
				value := spec.(*ast.ValueSpec)
				for i, name := range value.Names {
					object, _ := pass.TypesInfo.Defs[name].(*types.Var)
					if object == nil || name.Name == "_" {
						continue
					}
					if len(value.Values) == 0 {
						if _, ok := object.Type().Underlying().(*types.Struct); ok {
							pending[object] = name.Pos()
						}
					} else if len(value.Values) == len(value.Names) && newStruct(pass, value.Values[i]) {
						pending[object] = name.Pos()
					}
				}
			}
		case *ast.AssignStmt:
			for _, rhs := range n.Rhs {
				consume(rhs)
			}
			for i, lhs := range n.Lhs {
				if _, ok := ast.Unparen(lhs).(*ast.SelectorExpr); ok && n.Tok == token.ASSIGN {
					object := fieldRoot(pass, lhs)
					if pos, ok := pending[object]; ok {
						if !reported[pos] {
							pass.Reportf(pos, "construct %s with a struct literal instead of assigning its fields before first use", object.Name())
							reported[pos] = true
						}
						delete(pending, object)
					}
				}
				consume(lhs)
				id, ok := lhs.(*ast.Ident)
				if !ok || id.Name == "_" || len(n.Lhs) != len(n.Rhs) || !newStruct(pass, n.Rhs[i]) {
					continue
				}
				object, _ := pass.TypesInfo.ObjectOf(id).(*types.Var)
				if object != nil {
					pending[object] = id.Pos()
				}
			}
		default:
			// Control flow and closures can observe or escape a value. Be conservative;
			// still inspect fresh construction inside their individual blocks.
			consume(n)
			ast.Inspect(n, func(child ast.Node) bool {
				if _, ok := child.(*ast.FuncLit); ok {
					return false
				}
				if block, ok := child.(*ast.BlockStmt); ok {
					checkConstruction(pass, block)
					return false
				}
				return true
			})
		}
	}
	visit(body)
}
