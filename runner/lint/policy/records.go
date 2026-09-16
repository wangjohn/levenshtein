package policy

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// Records requires explicitly marked value records to be constructed together.
var Records = &analysis.Analyzer{
	Name:      "LV1002",
	Doc:       "construct types marked //levenshtein:record with struct literals instead of field assignments",
	Run:       runRecords,
	FactTypes: []analysis.Fact{new(recordFact)},
}

type recordFact struct{}

func (*recordFact) AFact() {}

func (*recordFact) String() string { return "record" }

func runRecords(pass *analysis.Pass) (any, error) {
	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			decl, ok := node.(*ast.GenDecl)
			if !ok || decl.Tok != token.TYPE {
				return true
			}
			for _, spec := range decl.Specs {
				spec := spec.(*ast.TypeSpec)
				if !recordComment(spec.Doc) && !recordComment(decl.Doc) {
					continue
				}
				object := pass.TypesInfo.Defs[spec.Name]
				if spec.Assign.IsValid() {
					pass.Reportf(spec.Pos(), "levenshtein:record must mark the original type, not an alias")
					continue
				}
				if object.Parent() != pass.Pkg.Scope() {
					pass.Reportf(spec.Pos(), "levenshtein:record requires a package-level struct type")
					continue
				}
				named, ok := types.Unalias(object.Type()).(*types.Named)
				if !ok {
					continue
				}
				if _, ok := named.Underlying().(*types.Struct); !ok {
					pass.Reportf(spec.Pos(), "levenshtein:record requires a struct type")
					continue
				}
				pass.ExportObjectFact(object, new(recordFact))
			}
			return false
		})
	}

	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			continue
		}
		ast.Inspect(file, func(node ast.Node) bool {
			switch node := node.(type) {
			case *ast.AssignStmt:
				for _, lhs := range node.Lhs {
					checkWrite(pass, lhs)
				}
			case *ast.IncDecStmt:
				checkWrite(pass, node.X)
			case *ast.RangeStmt:
				if node.Tok == token.ASSIGN {
					checkWrite(pass, node.Key)
					checkWrite(pass, node.Value)
				}
			case *ast.UnaryExpr:
				if node.Op == token.AND {
					checkWrite(pass, node.X)
				}
			}
			return true
		})
	}
	return nil, nil
}

func recordComment(group *ast.CommentGroup) bool {
	if group == nil {
		return false
	}
	for _, comment := range group.List {
		if strings.TrimSpace(comment.Text) == "//levenshtein:record" {
			return true
		}
	}
	return false
}

func checkWrite(pass *analysis.Pass, expr ast.Expr) {
	for expr != nil {
		switch node := ast.Unparen(expr).(type) {
		case *ast.SelectorExpr:
			selection := pass.TypesInfo.Selections[node]
			if selection == nil || selection.Kind() != types.FieldVal {
				return
			}
			t := selection.Recv()
			// A promoted field may belong to a marked embedded record.
			for _, index := range selection.Index() {
				t = types.Unalias(t)
				if pointer, ok := t.(*types.Pointer); ok {
					t = types.Unalias(pointer.Elem())
				}
				if named, ok := t.(*types.Named); ok && pass.ImportObjectFact(named.Obj(), new(recordFact)) {
					pass.Reportf(expr.Pos(), "construct %s with a struct literal instead of assigning its fields", named.Obj().Name())
					return
				}
				structure, ok := t.Underlying().(*types.Struct)
				if !ok {
					break
				}
				t = structure.Field(index).Type()
			}
			expr = node.X
		case *ast.IndexExpr:
			expr = node.X
		case *ast.StarExpr:
			expr = node.X
		default:
			return
		}
	}
}
