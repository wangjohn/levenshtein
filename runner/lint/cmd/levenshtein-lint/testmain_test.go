package main

import (
	"go/types"
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
