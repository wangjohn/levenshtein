// Package lvrules is the entry point Levenshtein compiles into its community
// linter when a consumer names this module in levenshtein.json.
package lvrules

import (
	"github.com/wangjohn/levenshtein/examples/rule-module/nopanic"
	"golang.org/x/tools/go/analysis"
)

// Namespace prefixes every rule this module contributes, so nopanic reports as
// example_nopanic. It is lowercase letters only and never changes once
// published: consumers' ignore directives and selections spell it.
const Namespace = "example"

// Analyzers returns the rules this module contributes.
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		nopanic.Analyzer,
	}
}
