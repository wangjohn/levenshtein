package semantic

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Item lists are what code preselects for the model. Each carries the line it
// was found on so a finding can point back into the file.
type commentItem struct {
	Comment string `json:"comment"`
	Code    string `json:"code"`
	Line    int    `json:"line"`
}

type errorItem struct {
	Call     string `json:"call"`
	Function string `json:"function"`
	Line     int    `json:"line"`
}

// goUnit is one top-level declaration touched by the change, with the items
// found inside its added lines.
type goUnit struct {
	Path       string
	Symbol     string
	Line       int
	After      string
	Diff       string
	AddedLines int
	Comments   []commentItem
	Errors     []errorItem
	New        bool
	IsFunc     bool
}

const (
	maxAfterChars = 30000
	maxDiffChars  = 30000
	maxItemChars  = 6000
	maxCodeLines  = 12
	maxNeighbours = 80
)

type goFile struct {
	fset  *token.FileSet
	file  *ast.File
	lines []string
	added map[int]bool
	docs  map[*ast.CommentGroup]bool
}

// goUnits parses the current file and groups hunks by enclosing declaration.
// Generated files and files that do not parse yield nothing.
func goUnits(path string, src []byte, hunks []Hunk) []goUnit {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil || ast.IsGenerated(file) {
		return nil
	}

	added := map[int]bool{}
	for _, hunk := range hunks {
		for line := hunk.NewStart; line < hunk.NewStart+hunk.NewLines; line++ {
			added[line] = true
		}
	}
	f := goFile{fset: fset, file: file, lines: strings.Split(string(src), "\n"), added: added, docs: docComments(file)}

	var units []goUnit
	for _, decl := range file.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
			continue
		}
		start, end := f.span(decl)
		if !f.touched(start, end) {
			continue
		}

		count := 0
		for line := start; line <= end; line++ {
			if added[line] {
				count++
			}
		}

		_, isFunc := decl.(*ast.FuncDecl)
		units = append(units, goUnit{
			Path:       path,
			Symbol:     symbol(decl),
			Line:       start,
			After:      truncate(f.text(start, end), maxAfterChars),
			Diff:       truncate(unitDiff(hunks, start, end), maxDiffChars),
			AddedLines: count,
			Comments:   f.comments(decl, start, end),
			Errors:     f.errors(decl),
			New:        added[f.line(decl.Pos())],
			IsFunc:     isFunc,
		})
	}
	return units
}

// unitDiff keeps only the hunk lines that fall inside one declaration, so a
// declaration in a new file does not carry the whole file's diff. Removed
// lines have no new-file position and are kept only when the hunk lies
// entirely inside the declaration.
func unitDiff(hunks []Hunk, start, end int) string {
	var diff strings.Builder
	for _, hunk := range hunks {
		last := hunk.NewStart + hunk.NewLines - 1
		if hunk.NewLines == 0 || hunk.NewStart > end || last < start {
			continue
		}
		if hunk.NewStart >= start && last <= end || len(hunk.Added) != hunk.NewLines {
			diff.WriteString(hunk.Diff)
			continue
		}

		first, stop := max(hunk.NewStart, start), min(last, end)
		fmt.Fprintf(&diff, "@@ +%d,%d @@ %s\n", first, stop-first+1, hunk.Header)
		for line := first; line <= stop; line++ {
			diff.WriteString("+" + hunk.Added[line-hunk.NewStart] + "\n")
		}
	}
	return diff.String()
}

func (f goFile) line(pos token.Pos) int {
	return f.fset.Position(pos).Line
}

// span covers the declaration and its doc comment.
func (f goFile) span(decl ast.Decl) (int, int) {
	start := decl.Pos()
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Doc != nil {
			start = d.Doc.Pos()
		}
	case *ast.GenDecl:
		if d.Doc != nil {
			start = d.Doc.Pos()
		}
	}
	return f.line(start), f.line(decl.End())
}

func (f goFile) touched(start, end int) bool {
	for line := start; line <= end; line++ {
		if f.added[line] {
			return true
		}
	}
	return false
}

func (f goFile) text(start, end int) string {
	if start < 1 {
		start = 1
	}
	if end > len(f.lines) {
		end = len(f.lines)
	}
	if start > end {
		return ""
	}
	return strings.Join(f.lines[start-1:end], "\n")
}

func (f goFile) nodeText(node ast.Node) string {
	return f.text(f.line(node.Pos()), f.line(node.End()))
}

func symbol(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil && len(d.Recv.List) > 0 {
			return receiverName(d.Recv.List[0].Type) + "." + d.Name.Name
		}
		return d.Name.Name
	case *ast.GenDecl:
		var names []string
		for _, spec := range d.Specs {
			switch s := spec.(type) {
			case *ast.TypeSpec:
				names = append(names, s.Name.Name)
			case *ast.ValueSpec:
				for _, name := range s.Names {
					names = append(names, name.Name)
				}
			}
		}
		if len(names) == 0 {
			return d.Tok.String()
		}
		if len(names) > 3 {
			names = append(names[:3], "...")
		}
		return strings.Join(names, ",")
	}
	return ""
}

func receiverName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return receiverName(t.X)
	case *ast.IndexListExpr:
		return receiverName(t.X)
	}
	return "?"
}

// docComments collects comment groups that document declarations, fields, and
// specs. Those legitimately restate what they annotate and are not judged.
func docComments(file *ast.File) map[*ast.CommentGroup]bool {
	docs := map[*ast.CommentGroup]bool{}
	note := func(groups ...*ast.CommentGroup) {
		for _, group := range groups {
			if group != nil {
				docs[group] = true
			}
		}
	}
	note(file.Doc)
	ast.Inspect(file, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.FuncDecl:
			note(n.Doc)
		case *ast.GenDecl:
			note(n.Doc)
		case *ast.TypeSpec:
			note(n.Doc, n.Comment)
		case *ast.ValueSpec:
			note(n.Doc, n.Comment)
		case *ast.ImportSpec:
			note(n.Doc, n.Comment)
		case *ast.Field:
			note(n.Doc, n.Comment)
		}
		return true
	})
	return docs
}

func directive(text string) bool {
	return strings.HasPrefix(text, "//go:") || strings.HasPrefix(text, "//lint:") || strings.HasPrefix(text, "//nolint") || strings.HasPrefix(text, "//levenshtein:") || strings.HasPrefix(text, "//line ")
}

func (f goFile) comments(decl ast.Decl, start, end int) []commentItem {
	statements := f.statements(decl)

	var items []commentItem
	for _, group := range f.file.Comments {
		first, last := f.line(group.Pos()), f.line(group.End())
		if first < start || last > end || f.docs[group] || directive(group.List[0].Text) {
			continue
		}
		if !f.touched(first, last) {
			continue
		}
		code := f.attachedCode(statements, first, last)
		if code == "" {
			continue
		}
		items = append(items, commentItem{Comment: strings.TrimSpace(group.Text()), Code: truncate(code, maxItemChars), Line: first})
	}
	return items
}

// statements lists every statement in a function body, innermost included,
// so a comment can be paired with the code it annotates.
func (f goFile) statements(decl ast.Decl) []ast.Stmt {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok || fn.Body == nil {
		return nil
	}

	var statements []ast.Stmt
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		stmt, ok := node.(ast.Stmt)
		if ok {
			if _, block := stmt.(*ast.BlockStmt); !block {
				statements = append(statements, stmt)
			}
		}
		return true
	})
	return statements
}

// attachedCode picks the statement on a trailing comment's line, or the
// outermost statement starting right after a leading comment.
func (f goFile) attachedCode(statements []ast.Stmt, first, last int) string {
	var trailing, leading ast.Stmt
	for _, stmt := range statements {
		start, end := f.line(stmt.Pos()), f.line(stmt.End())
		if start <= first && end >= first && start != first+1 {
			if trailing == nil || (f.line(trailing.End())-f.line(trailing.Pos())) > end-start {
				trailing = stmt
			}
		}
		if start == last+1 {
			if leading == nil || end > f.line(leading.End()) {
				leading = stmt
			}
		}
	}

	chosen := leading
	if chosen == nil {
		chosen = trailing
	}
	if chosen == nil {
		return f.text(last+1, last+3)
	}
	start, end := f.line(chosen.Pos()), f.line(chosen.End())
	if end-start+1 > maxCodeLines {
		end = start + maxCodeLines - 1
	}
	return f.text(start, end)
}

func (f goFile) errors(decl ast.Decl) []errorItem {
	fn, ok := decl.(*ast.FuncDecl)
	if !ok || fn.Body == nil {
		return nil
	}

	var items []errorItem
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || !f.added[f.line(call.Pos())] {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || !errorConstructor(pkg.Name, selector.Sel.Name) {
			return true
		}
		items = append(items, errorItem{Call: truncate(f.nodeText(call), maxItemChars), Function: symbol(decl), Line: f.line(call.Pos())})
		return true
	})
	return items
}

var errorConstructors = map[string]bool{
	"fmt.Errorf": true,
	"errors.New": true,
}

func errorConstructor(pkg, name string) bool {
	return errorConstructors[pkg+"."+name]
}

// packageFunctions lists top-level function signatures in the directory so
// the model can compare new code against existing helpers.
func packageFunctions(dir, current string, isTest bool, exclude map[string]bool) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	var signatures []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || (!isTest && strings.HasSuffix(name, "_test.go")) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, name, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil || ast.IsGenerated(file) {
			continue
		}
		lines := strings.Split(string(src), "\n")
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || (name == filepath.Base(current) && exclude[symbol(fn)]) {
				continue
			}
			end := fn.End()
			if fn.Body != nil {
				end = fn.Body.Pos()
			}
			startLine, endLine := fset.Position(fn.Pos()).Line, fset.Position(end).Line
			signature := strings.TrimSpace(strings.TrimSuffix(strings.Join(lines[startLine-1:endLine], " "), "{"))
			if fn.Doc != nil && len(fn.Doc.List) > 0 {
				signature += "  " + strings.TrimSpace(fn.Doc.List[0].Text)
			}
			signatures = append(signatures, signature)
		}
	}

	sort.Strings(signatures)
	if len(signatures) > maxNeighbours {
		signatures = signatures[:maxNeighbours]
	}
	return signatures
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "\n... [truncated]"
}
