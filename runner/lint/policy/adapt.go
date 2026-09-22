package policy

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
)

// Adapt makes upstream analyzers behave like the shared rules do.
//
// Staticcheck's runner keys a diagnostic by its category and drops the ones
// whose category is not a registered check, so an analyzer that categorizes its
// own findings (nilness, perfsprint) would report nothing. Clearing the category
// restores the analyzer's name as the reported code. Generated files stay silent
// because nobody edits them, while the analyzer still runs there so the facts it
// exports for hand-written callers remain correct.
func Adapt(analyzers ...*analysis.Analyzer) []*analysis.Analyzer {
	for _, current := range analyzers {
		run := current.Run
		current.Run = func(pass *analysis.Pass) (any, error) {
			generated := generatedFiles(pass)
			report := pass.Report

			adapted := *pass
			adapted.Report = func(diagnostic analysis.Diagnostic) {
				if generated[pass.Fset.File(diagnostic.Pos)] {
					return
				}
				diagnostic.Category = ""
				report(diagnostic)
			}
			return run(&adapted)
		}
	}
	return analyzers
}

func generatedFiles(pass *analysis.Pass) map[*token.File]bool {
	generated := map[*token.File]bool{}
	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			generated[pass.Fset.File(file.FileStart)] = true
		}
	}
	return generated
}
