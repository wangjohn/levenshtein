package nopanic_test

import (
	"testing"

	"github.com/wangjohn/levenshtein/examples/rule-module/nopanic"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestAnalyzer(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), nopanic.Analyzer, "library", "app", "shadowed")
}
