package policy

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestTypedValues(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), TypedValues, "typed")
}

func TestRecords(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Records, "records", "consumer")
}

func TestFields(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Fields, "fields")
}

func TestSpacing(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Spacing, "spacing")
}

func TestFormatting(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Formatting, "formatting")
}

func TestAssertions(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Assertions, "assertions")
}
