package main

import (
	"go/ast"
	"go/build"
	"go/token"
	"path/filepath"
	"slices"
	"strings"

	"github.com/wangjohn/levenshtein/runner/lint/policy"
	"golang.org/x/tools/go/analysis"
	"honnef.co/go/tools/analysis/lint"
)

// Staticcheck lints a package with tests in two builds, without its tests and
// with them, and judges each //lint:ignore directive in each build on its own:
// a directive that matches no finding in one build is reported as unused,
// whatever the other build found. Most findings on a non-test file are the
// same in both builds, but some analyzers see the test files too: a pointer
// receiver declared in a test file makes recvcheck report the type, a test's
// calls change what unparam concludes about a parameter, and a test's variant
// of a sum type leaves gochecksumtype's switches incomplete. A directive for
// such a finding would be reported as unused by the other build, so it could
// never suppress the finding. Staticcheck exempts its own U1000 the same way.
//
// For those analyzers, one build decides the findings and directives on the
// lines of non-test files, and the other build leaves those lines alone: it
// reports none of its own findings there and matches every directive for the
// analyzer there with a finding the directive then hides. A directive that
// matches nothing in the deciding build is still reported as unused.

// judge names the build that decides a test-sensitive analyzer's findings on
// the lines of a package's non-test files.
type judge string

const (
	// withoutTests decides by the package's own code: a test cannot make a
	// parameter or a switch look wrong. The build with tests drops its
	// findings on non-test files.
	withoutTests judge = "without tests"
	// withTests decides with the tests' code included, which only adds to what
	// the build without tests finds, so no finding is lost.
	withTests judge = "with tests"
)

// perBuild makes analyzer judge the lines of non-test files in one build only.
func perBuild(analyzer *analysis.Analyzer, deciding judge) *analysis.Analyzer {
	run := analyzer.Run
	analyzer.Run = func(pass *analysis.Pass) (any, error) {
		var plain []*ast.File
		test := map[*token.File]bool{}
		for _, file := range pass.Files {
			if strings.HasSuffix(policy.SourceName(pass.Fset, file), "_test.go") {
				test[pass.Fset.File(file.FileStart)] = true
			} else {
				plain = append(plain, file)
			}
		}
		// The build with tests of a package whose tests are all external holds
		// only test files, so it never has lines to leave alone.
		withTestFiles := len(test) > 0

		switch {
		case deciding == withoutTests && withTestFiles:
			report := pass.Report
			testsOnly := *pass
			testsOnly.Report = func(diagnostic analysis.Diagnostic) {
				if test[pass.Fset.File(diagnostic.Pos)] {
					report(diagnostic)
				}
			}
			matchDirectives(pass, ignores(pass, plain), deciding)
			return run(&testsOnly)
		case deciding == withTests && !withTestFiles:
			// Only tests in the package itself give it a build with tests that
			// sees these files, and go/build is asked only when there is a
			// directive to leave to that build. The cache does not key on
			// -tests, so the directives are left to it under -tests=false too,
			// where an unused one then goes unreported.
			if directives := ignores(pass, plain); len(directives) > 0 && internalTests(pass) {
				matchDirectives(pass, directives, deciding)
			}
		}
		return run(pass)
	}
	return analyzer
}

// matchDirectives reports a finding under each directive, which the directive
// hides, so this build does not report the directive as unused and leaves that
// to the deciding build.
func matchDirectives(pass *analysis.Pass, directives []lint.Directive, deciding judge) {
	for _, directive := range directives {
		pass.Report(analysis.Diagnostic{
			Pos:     directive.Node.Pos(),
			Message: "this //lint:ignore " + pass.Analyzer.Name + " is judged in the build " + string(deciding),
		})
	}
}

// ignores returns the //lint:ignore directives in files that name the
// analyzer, matched as Staticcheck matches a finding's code: a
// case-insensitive glob per comma-separated name.
func ignores(pass *analysis.Pass, files []*ast.File) []lint.Directive {
	var commented []*ast.File
	for _, file := range files {
		if slices.ContainsFunc(file.Comments, hasIgnore) {
			commented = append(commented, file)
		}
	}

	var named []lint.Directive
	for _, directive := range lint.ParseDirectives(commented, pass.Fset) {
		if directive.Command != "ignore" || len(directive.Arguments) == 0 {
			continue
		}
		for check := range strings.SplitSeq(directive.Arguments[0], ",") {
			if matched, _ := filepath.Match(strings.ToLower(check), strings.ToLower(pass.Analyzer.Name)); matched {
				named = append(named, directive)
				break
			}
		}
	}
	return named
}

// hasIgnore reports whether a comment group holds a //lint:ignore line. Only
// files with one are parsed for directives, which builds a comment map.
func hasIgnore(group *ast.CommentGroup) bool {
	return slices.ContainsFunc(group.List, func(comment *ast.Comment) bool {
		return strings.HasPrefix(comment.Text, "//lint:ignore ")
	})
}

// internalTests reports whether the package's directory has test files in the
// package itself, which Staticcheck lints in a build with tests alongside the
// package's own files. A directory go/build cannot read is taken to have none,
// so the build without tests keeps judging its directives.
func internalTests(pass *analysis.Pass) bool {
	if len(pass.Files) == 0 {
		return false
	}
	listed, _ := build.ImportDir(filepath.Dir(policy.SourceName(pass.Fset, pass.Files[0])), 0)
	return listed != nil && len(listed.TestGoFiles) > 0
}
