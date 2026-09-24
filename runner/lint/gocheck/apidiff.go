package gocheck

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/mod/modfile"
)

// reportSection is one of the headers apidiff's text report groups changes
// under.
type reportSection string

const (
	incompatibleHeader reportSection = "Incompatible changes:"
	compatibleHeader   reportSection = "Compatible changes:"
)

// The stderr line apidiff prints for each internal package it leaves out of a
// module comparison, and how many compatible changes a report lists.
const (
	ignoredInternal    = "Ignoring internal package "
	maxCompatibleNotes = 100
)

// APIChange is one line of apidiff's report.
type APIChange struct {
	Message    string
	Compatible bool
}

// Apidiff compares the exported API of the module at module in the base and
// head trees with the pinned apidiff at tool, and reports each incompatible
// change as a finding at the declaration it concerns in the head tree, or at
// its package when the declaration is gone. Compatible changes are notes.
//
// apidiff leaves internal packages out of a module comparison itself. Nothing
// can import a main package, so its changes are dropped here: every change to
// a package that was a command at the base, and additions to one that is a
// command now. A module missing at the base, or whose path changed, as it does
// for a new major version, has no API to break, and passes with a note.
//
// workspace is the go.work the head tree is analyzed with, relative to its
// root, or "off"; the base uses the same file when it has one.
func Apidiff(ctx context.Context, tool, base, head, module, workspace string, env []string) (Report, error) {
	headDir := filepath.Join(head, filepath.FromSlash(module))
	baseDir := filepath.Join(base, filepath.FromSlash(module))
	headPath, err := modulePath(headDir)
	if err != nil {
		return Report{}, err
	}
	if _, err := os.Stat(filepath.Join(baseDir, "go.mod")); errors.Is(err, fs.ErrNotExist) {
		return Report{Notes: []string{fmt.Sprintf("module %s did not exist at the base, so it has no API to break", headPath)}}, nil
	}
	basePath, err := modulePath(baseDir)
	if err != nil {
		return Report{}, err
	}
	if basePath != headPath {
		return Report{Notes: []string{fmt.Sprintf("the module path changed from %s to %s, so importers of the old path are unaffected; nothing was compared", basePath, headPath)}}, nil
	}

	exports, err := os.MkdirTemp("", "levenshtein-apidiff-")
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = os.RemoveAll(exports) }() // Export data cleanup.

	sides := []struct {
		name   string
		dir    string
		env    []string
		export string
	}{
		{"base", baseDir, withEnv(env, "GOWORK="+workFile(base, workspace)), filepath.Join(exports, "base")},
		{"head", headDir, withEnv(env, "GOWORK="+workFile(head, workspace)), filepath.Join(exports, "head")},
	}
	mains := make([]map[string]bool, len(sides))
	for i, side := range sides {
		wrote, err := command(ctx, side.dir, side.env, tool, "-m", "-w", side.export, headPath)
		if err != nil {
			return Report{}, err
		}
		if wrote.ExitCode != 0 {
			return Report{}, fmt.Errorf("apidiff could not load the %s module: %s", side.name, wrote.output())
		}
		packages, err := listPackages(ctx, side.dir, side.env)
		if err != nil {
			return Report{}, fmt.Errorf("listing the %s module's packages: %w", side.name, err)
		}
		mains[i] = map[string]bool{}
		for _, pkg := range packages {
			mains[i][pkg.ImportPath] = pkg.Name == "main"
		}
	}

	compared, err := command(ctx, headDir, env, tool, "-m", sides[0].export, sides[1].export)
	if err != nil {
		return Report{}, err
	}
	if compared.ExitCode != 0 {
		return Report{}, fmt.Errorf("apidiff exited %d: %s", compared.ExitCode, compared.output())
	}
	for line := range strings.Lines(compared.Stderr) {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, ignoredInternal) {
			return Report{}, fmt.Errorf("apidiff printed an unexpected warning: %s", compared.output())
		}
	}
	changes, err := ParseAPIChanges(compared.Stdout)
	if err != nil {
		return Report{}, err
	}

	var findings []Finding
	var compatible []string
	for _, change := range changes {
		pkg, object := changeSubject(change.Message, headPath)
		if mains[0][pkg] || (change.Compatible && mains[1][pkg]) {
			continue
		}
		if change.Compatible {
			compatible = append(compatible, change.Message)
			continue
		}
		findings = append(findings, Finding{
			Code:     CodeApidiff,
			Message:  fmt.Sprintf("incompatible change to the exported API of %s: %s", pkg, change.Message),
			Location: declaration(head, module, headPath, pkg, object),
		})
	}

	sortFindings(findings)
	slices.Sort(compatible)
	notes := []string{fmt.Sprintf("%d incompatible and %s to the exported API of %s; internal and main packages are not compared", len(findings), count(len(compatible), "compatible change"), headPath)}
	for i, change := range compatible {
		if i == maxCompatibleNotes {
			notes = append(notes, fmt.Sprintf("compatible: [%d more]", len(compatible)-i))
			break
		}
		notes = append(notes, "compatible: "+change)
	}
	return Report{Findings: findings, Notes: notes}, nil
}

// modulePath reads the module path from dir/go.mod.
func modulePath(dir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("the module needs a readable go.mod: %w", err)
	}
	modPath := modfile.ModulePath(data)
	if modPath == "" {
		return "", fmt.Errorf("%s declares no module path", filepath.Join(dir, "go.mod"))
	}
	return modPath, nil
}

// workFile is the GOWORK value for one tree: its copy of the workspace file,
// when the head is analyzed in a workspace and the tree has one, and "off"
// otherwise.
func workFile(root, workspace string) string {
	if workspace == "" || workspace == "off" {
		return "off"
	}
	file := filepath.Join(root, filepath.FromSlash(workspace))
	if info, err := os.Stat(file); err == nil && info.Mode().IsRegular() {
		return file
	}
	return "off"
}

// ParseAPIChanges reads apidiff's text report: a header for each kind of
// change, then one "- " line per change. apidiff also prints a three-line
// warning when it has two messages for one object; that is skipped. Anything
// else means the report is not what this parser understands, which is an
// error rather than a pass.
func ParseAPIChanges(report string) ([]APIChange, error) {
	var changes []APIChange
	var section reportSection
	for line := range strings.Lines(report) {
		line = strings.TrimRight(line, "\r\n")
		switch header := reportSection(line); {
		case line == "":
		case header == incompatibleHeader || header == compatibleHeader:
			section = header
		case strings.HasPrefix(line, "! second, different message"), strings.HasPrefix(line, "  first:"), strings.HasPrefix(line, "  second:"):
		case strings.HasPrefix(line, "- ") && section != "":
			changes = append(changes, APIChange{Message: strings.TrimPrefix(line, "- "), Compatible: section == compatibleHeader})
		default:
			return nil, fmt.Errorf("apidiff printed a line this check does not understand: %q", line)
		}
	}
	return changes, nil
}

// changeSubject finds the package and the object a change message is about. A
// module comparison names an object in the module's root package bare, one in
// a package below it as "./sub.Name", and a whole package as "package path".
func changeSubject(message, module string) (string, string) {
	if rest, ok := strings.CutPrefix(message, "package "); ok {
		pkg, _, _ := strings.Cut(rest, ": ")
		return pkg, ""
	}
	subject, _, _ := strings.Cut(message, ": ")
	rest, local := strings.CutPrefix(subject, "./")
	if !local && !strings.Contains(subject, "/") {
		return module, subject
	}
	slash := strings.LastIndex(rest, "/")
	dot := strings.Index(rest[slash+1:], ".")
	if dot < 0 {
		return module, ""
	}
	pkg, object := rest[:slash+1+dot], rest[slash+2+dot:]
	if local {
		pkg = module + "/" + pkg
	}
	return pkg, object
}

// declaration locates a change in the head tree: at the declaration of the
// object it names, else at the package clause of the package's first file,
// else, for a package that no longer exists, at the module's go.mod.
func declaration(head, module, modPath, pkg, object string) Location {
	rel, ok := strings.CutPrefix(pkg, modPath)
	fallback := Location{File: path.Join(module, "go.mod"), Line: 1}
	if !ok || (rel != "" && !strings.HasPrefix(rel, "/")) {
		return fallback
	}
	dir := path.Join(module, strings.TrimPrefix(rel, "/"))
	entries, err := os.ReadDir(filepath.Join(head, filepath.FromSlash(dir)))
	if err != nil {
		return fallback
	}

	fset := token.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		file := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, filepath.Join(head, filepath.FromSlash(dir), file), nil, parser.SkipObjectResolution)
		if err == nil {
			files = append(files, parsed)
		}
	}
	if len(files) == 0 {
		return fallback
	}

	at := func(pos token.Pos) Location {
		position := fset.Position(pos)
		return Location{File: path.Join(dir, filepath.Base(position.Filename)), Line: position.Line, Column: position.Column}
	}
	name, member := objectNames(object)
	// A method can be declared in any file of the package, so look for it
	// everywhere before settling for its type.
	for _, lookup := range []func(*ast.File) token.Pos{
		func(file *ast.File) token.Pos { return method(file, name, member) },
		func(file *ast.File) token.Pos { return declared(file, name) },
	} {
		for _, file := range files {
			if pos := lookup(file); pos.IsValid() {
				return at(pos)
			}
		}
	}
	return at(files[0].Package)
}

// objectNames splits "(*T).M", "T.M" or "F" into the top-level name and the
// member after it, if any.
func objectNames(object string) (string, string) {
	object = strings.TrimLeft(object, "(*")
	end := strings.IndexFunc(object, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	if end < 0 {
		return object, ""
	}
	name, rest := object[:end], strings.TrimLeft(object[end:], ")")
	member, _ := strings.CutPrefix(rest, ".")
	end = strings.IndexFunc(member, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' })
	if end >= 0 {
		member = member[:end]
	}
	return name, member
}

// method finds the declaration of the method member on the type name.
func method(file *ast.File, name, member string) token.Pos {
	if name == "" || member == "" {
		return token.NoPos
	}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv != nil && fn.Name.Name == member && receiver(fn.Recv) == name {
			return fn.Name.Pos()
		}
	}
	return token.NoPos
}

// declared finds the top-level declaration of name: a function, type,
// variable, or constant.
func declared(file *ast.File, name string) token.Pos {
	if name == "" {
		return token.NoPos
	}
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.FuncDecl:
			if decl.Recv == nil && decl.Name.Name == name {
				return decl.Name.Pos()
			}
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					if spec.Name.Name == name {
						return spec.Name.Pos()
					}
				case *ast.ValueSpec:
					if i := slices.IndexFunc(spec.Names, func(ident *ast.Ident) bool { return ident.Name == name }); i >= 0 {
						return spec.Names[i].Pos()
					}
				}
			}
		}
	}
	return token.NoPos
}

// receiver is the base type name of a method's receiver.
func receiver(fields *ast.FieldList) string {
	if len(fields.List) == 0 {
		return ""
	}
	expr := fields.List[0].Type
	for {
		switch typed := expr.(type) {
		case *ast.StarExpr:
			expr = typed.X
		case *ast.IndexExpr:
			expr = typed.X
		case *ast.IndexListExpr:
			expr = typed.X
		case *ast.Ident:
			return typed.Name
		default:
			return ""
		}
	}
}
