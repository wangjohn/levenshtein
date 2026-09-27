package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/lintcmd"
)

func TestOnlyGeneratedTestMainsSkipMusttag(t *testing.T) {
	for _, test := range []struct {
		path string
		name string
		want bool
	}{
		{"example.com/app.test", "main", true},
		{"example.com/app", "main", false},
		{"example.com/app.test", "app", false},
	} {
		pass := &analysis.Pass{Pkg: types.NewPackage(test.path, test.name)}

		if got := testMain(pass); got != test.want {
			t.Errorf("testMain(%s, package %s) = %v, want %v", test.path, test.name, got, test.want)
		}
	}
}

func TestOnlyALintRunTouchesTheCache(t *testing.T) {
	for _, test := range []struct {
		args []string
		want bool
	}{
		{[]string{"./..."}, true},
		{[]string{"-f=json", "-checks=all", "./..."}, true},
		{[]string{"-list-checks"}, false},
		{[]string{"-explain", "SA4006"}, false},
		{[]string{"-version"}, false},
	} {
		command := lintcmd.NewCommand("levenshtein-lint")
		command.ParseFlags(test.args)

		if got := linting(command.FlagSet()); got != test.want {
			t.Errorf("linting(%v) = %v, want %v", test.args, got, test.want)
		}
	}
}

// Staticcheck's runner leaves Pass.Module unset, so LV1001 gets the module from
// the go.mod above the package's files, and a driver's own module wins.
func TestHouseRulesSeeTheModule(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/app\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, filepath.Join(dir, "app.go"), "package app\n", 0)
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		module *analysis.Module
		want   string
	}{
		{nil, "example.com/app"},
		{&analysis.Module{Path: "example.com/driver"}, "example.com/driver"},
	} {
		var got string
		analyzer := inModule(&analysis.Analyzer{
			Name: "module",
			Doc:  "record the module",
			Run: func(pass *analysis.Pass) (any, error) {
				got = pass.Module.Path
				return nil, nil
			},
		})
		pass := &analysis.Pass{Fset: fset, Files: []*ast.File{file}, Pkg: types.NewPackage("example.com/app", "app"), Module: test.module}
		if _, err := analyzer.Run(pass); err != nil {
			t.Fatal(err)
		}

		if got != test.want {
			t.Errorf("module with Pass.Module %v = %q, want %q", test.module, got, test.want)
		}
	}
}
