package main

import (
	"testing"

	"github.com/wangjohn/levenshtein/runner/lint/policy"
	"golang.org/x/tools/go/analysis/analysistest"
)

// A package that imports "C" is analyzed through cgo's rewrite of each file,
// which opens with a generated header. The wrappers that look for generated
// files must still check the hand-written file of each fixture and leave its
// generated file alone.
func TestCgoFiles(t *testing.T) {
	t.Setenv("CGO_ENABLED", "1")
	analysistest.Run(t, analysistest.TestData(), receivers(), "cgoreceivers")
	analysistest.Run(t, analysistest.TestData(), policy.Adapt(unusedParams())[0], "cgoparams")
}
