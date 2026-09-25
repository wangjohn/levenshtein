package policy

import "golang.org/x/tools/go/analysis"

// Adapt makes upstream analyzers behave like the shared rules do: each one
// reports under its own name and stays silent in generated files (see adapt).
func Adapt(analyzers ...*analysis.Analyzer) []*analysis.Analyzer {
	for _, current := range analyzers {
		adapt(current)
	}
	return analyzers
}
