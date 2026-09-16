package main

import "testing"

func TestLintExitStatusAndDiagnosticsAgree(t *testing.T) {
	valid := `{"code":"SA5001","message":"defer before error check","location":{"file":"/src/close.go","line":7,"column":2}}`
	findings, err := parseFindings(1, valid, "", []string{"SA5001"})
	if err != nil || len(findings) != 1 || findings[0].Location.File != "close.go" {
		t.Fatalf("lost lint diagnostic: %v, %v", findings, err)
	}
	for _, tc := range []struct {
		code        int
		out, stderr string
	}{
		{0, valid, ""}, {1, "", ""}, {2, "", "crash"},
		{1, "not JSON", ""}, {0, "", "warning: no packages"},
		{1, `{"code":"compile","message":"syntax error"}`, ""},
	} {
		if _, err := parseFindings(tc.code, tc.out, tc.stderr, []string{"SA5001"}); err == nil {
			t.Errorf("accepted invalid tool result: %+v", tc)
		}
	}
	if _, err := parseFindings(0, "", "", []string{"SA5001"}); err != nil {
		t.Fatal(err)
	}
}
