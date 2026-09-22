package main

import (
	"go/version"
	"path/filepath"
	"sync"

	"github.com/go-critic/go-critic/checkers"
	"github.com/go-critic/go-critic/linter"
	"golang.org/x/tools/go/analysis"
)

// offCritics are stable diagnostic go-critic checkers left out because a rule
// already on reports the same mistake on the same line. docs/checks.md records
// the evidence for each.
var offCritics = map[string]bool{
	"caseOrder":        true, // SA4020
	"dupSubExpr":       true, // SA4000
	"sloppyLen":        true, // SA4024; its only other finding is spelling
	"sloppyTypeAssert": true, // S1040
}

// experimentalCritics are diagnostic checkers go-critic still tags
// experimental that join the selection anyway: nothing else reports what they
// find, and a false alarm is rare.
var experimentalCritics = map[string]bool{
	"badRegexp":    true,
	"filepathJoin": true,
}

// embeddedRules registers the go-critic checkers written as ruleguard rules,
// which go-critic leaves to its caller and refuses to register twice.
var embeddedRules = sync.OnceValue(checkers.InitEmbeddedRules)

// critics returns go-critic's diagnostic checkers, each as its own analyzer,
// so a finding carries the checker's name as its code and one checker can be
// deselected or suppressed without the rest.
func critics() []*analysis.Analyzer {
	if err := embeddedRules(); err != nil {
		panic(err)
	}

	var analyzers []*analysis.Analyzer
	for _, info := range linter.GetCheckersInfo() {
		if criticSelected(info) {
			analyzers = append(analyzers, critic(info))
		}
	}
	return analyzers
}

// criticSelected keeps go-critic's diagnostic tag, which marks likely bugs
// rather than style or performance advice, minus the checkers in offCritics
// and the experimental ones upstream has not settled.
func criticSelected(info *linter.CheckerInfo) bool {
	if !info.HasTag(linter.DiagnosticTag) || offCritics[info.Name] {
		return false
	}
	return !info.HasTag(linter.ExperimentalTag) || experimentalCritics[info.Name]
}

// critic runs one go-critic checker over a package the way go-critic's own
// analyzer runs its whole selection.
func critic(info *linter.CheckerInfo) *analysis.Analyzer {
	return &analysis.Analyzer{
		Name: info.Name,
		Doc:  info.Summary,
		Run: func(pass *analysis.Pass) (any, error) {
			shared := linter.NewContext(pass.Fset, pass.TypesSizes)
			shared.SetGoVersion(version.Lang(pass.Pkg.GoVersion()))
			shared.SetPackageInfo(pass.TypesInfo, pass.Pkg)

			checker, err := linter.NewChecker(shared, info)
			if err != nil {
				return nil, err
			}
			for _, file := range pass.Files {
				shared.SetFileInfo(filepath.Base(pass.Fset.File(file.FileStart).Name()), file)
				for _, warning := range checker.Check(file) {
					pass.Report(diagnostic(warning))
				}
			}
			return nil, nil
		},
	}
}

// diagnostic carries a go-critic warning and its quick fix, when it has one.
func diagnostic(warning linter.Warning) analysis.Diagnostic {
	var fixes []analysis.SuggestedFix
	if warning.HasQuickFix() {
		fixes = []analysis.SuggestedFix{{
			Message: "suggested replacement",
			TextEdits: []analysis.TextEdit{{
				Pos:     warning.Suggestion.From,
				End:     warning.Suggestion.To,
				NewText: warning.Suggestion.Replacement,
			}},
		}}
	}
	return analysis.Diagnostic{
		Pos:            warning.Pos,
		Message:        warning.Text,
		SuggestedFixes: fixes,
	}
}
