package main

import (
	"slices"
	"testing"

	"github.com/go-critic/go-critic/linter"
)

// TestCriticSelection pins the go-critic checkers the linter runs, so a
// go-critic upgrade that adds, renames, or retags a checker shows up in review
// together with docs/checks.md.
func TestCriticSelection(t *testing.T) {
	var names []string
	for _, analyzer := range critics() {
		names = append(names, analyzer.Name)
	}

	want := []string{
		"appendAssign", "argOrder", "badCall", "badCond", "badRegexp", "badSyncOnceFunc", "codegenComment", "deferInLoop",
		"deprecatedComment", "dupArg", "dupBranchBody", "dupCase", "evalOrder", "exitAfterDefer", "filepathJoin", "flagDeref",
		"flagName", "mapKey", "offBy1", "rangeAppendAll", "returnAfterHttpError",
	}
	if !slices.Equal(names, want) {
		t.Errorf("go-critic checkers = %v, want %v", names, want)
	}
}

// TestCriticExceptions keeps the exception lists naming real checkers: a name
// go-critic no longer registers would make an exception silently do nothing.
func TestCriticExceptions(t *testing.T) {
	if err := embeddedRules(); err != nil {
		t.Fatal(err)
	}

	registered := map[string]*linter.CheckerInfo{}
	for _, info := range linter.GetCheckersInfo() {
		registered[info.Name] = info
	}

	for name := range offCritics {
		info := registered[name]
		if info == nil || !info.HasTag(linter.DiagnosticTag) || info.HasTag(linter.ExperimentalTag) {
			t.Errorf("offCritics names %s, which is not a stable diagnostic checker", name)
		}
	}
	for name := range experimentalCritics {
		info := registered[name]
		if info == nil || !info.HasTag(linter.DiagnosticTag) || !info.HasTag(linter.ExperimentalTag) {
			t.Errorf("experimentalCritics names %s, which is not an experimental diagnostic checker", name)
		}
	}
}
