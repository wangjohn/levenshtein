// Package levenshtein is the entry point Levenshtein compiles into its linter
// when a consumer names this module in levenshtein.json.
package levenshtein

import (
	"github.com/wangjohn/levenshtein/examples/rule-module/nopanic"
	"golang.org/x/tools/go/analysis"
)

// Analyzers returns the rules this module contributes. Levenshtein prefixes
// each name with the consumer's namespace, so nopanic reports as acme_nopanic
// for a consumer that registers the module as "acme".
func Analyzers() []*analysis.Analyzer {
	return []*analysis.Analyzer{
		nopanic.Analyzer,
	}
}
