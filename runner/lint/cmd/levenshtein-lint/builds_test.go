package main

import (
	"testing"

	"github.com/wangjohn/levenshtein/runner/lint/policy"
	"golang.org/x/tools/go/analysis/analysistest"
)

// analysistest checks each build of a package on its own, so a want comment in
// a non-test file must match what the build without tests and the build with
// tests each report there: one build's finding, and the other build's match
// for the directive above it.
func TestOneBuildJudgesTestSensitiveFindings(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), receivers(), "buildreceivers", "buildnotests")
	analysistest.Run(t, analysistest.TestData(), policy.Adapt(unusedParams())[0], "buildparams")
	analysistest.Run(t, analysistest.TestData(), sumTypes(), "buildsumtypes")
}
