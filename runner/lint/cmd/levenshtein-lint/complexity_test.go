package main

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestCognitiveThreshold pins gocognit's line at 30: a function at 30 passes
// and one at 31 is reported. Its -over flag defaults to 0, which would report
// every function.
func TestCognitiveThreshold(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), cognitive(), "cognitive")
}
