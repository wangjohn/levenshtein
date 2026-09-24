package verify

import (
	"encoding/json"
	"testing"
)

// Hints and the baseline rewrite findings in place, so every other field of a
// result's details, such as go-mutation's summary, must survive the rewrite.
func TestReplaceFindingsKeepsTheOtherFields(t *testing.T) {
	findings := []finding{{Code: "SA4006", Message: "unused", Location: location{File: "a.go", Line: 3}}}

	for name, tc := range map[string]struct {
		details string
		want    string
	}{
		"other fields": {`{"findings":[],"summary":{"killed":2}}`, `{"findings":[{"code":"SA4006","message":"unused","location":{"file":"a.go","line":3,"column":0}}],"summary":{"killed":2}}`},
		"no details":   {``, `{"findings":[{"code":"SA4006","message":"unused","location":{"file":"a.go","line":3,"column":0}}]}`},
		"null details": {`null`, `{"findings":[{"code":"SA4006","message":"unused","location":{"file":"a.go","line":3,"column":0}}]}`},
		"not a record": {`["raw"]`, `["raw"]`},
	} {
		got := replaceFindings(json.RawMessage(tc.details), findings)
		if string(got) != tc.want {
			t.Errorf("%s: got %s, want %s", name, got, tc.want)
		}
	}
}
