package community

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestMixedDirectives(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), mixedAnalyzer(), "mixed")
}

func TestRenamedDirectives(t *testing.T) {
	renames := map[string]string{"errs_panics": "errs_nopanic"}

	analysistest.Run(t, analysistest.TestData(), renamedAnalyzer(renames), "renamed")
}
