package main

import (
	"testing"

	"github.com/gostaticanalysis/nilerr"
	"golang.org/x/tools/go/analysis/analysistest"
)

// TestIgnoresAreLeftToStaticcheck keeps nilerr reporting a finding under
// //lint:ignore nilerr. nilerr drops it itself upstream, which leaves
// Staticcheck's directive matching nothing and the finding impossible to
// suppress.
func TestIgnoresAreLeftToStaticcheck(t *testing.T) {
	analyzer := *nilerr.Analyzer
	analysistest.Run(t, analysistest.TestData(), upstream(&analyzer)[0], "ignored")
}
