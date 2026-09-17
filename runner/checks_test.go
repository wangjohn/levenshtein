package main

import "testing"

func TestCommandFailuresDoNotBecomePassingResults(t *testing.T) {
	for _, tc := range []struct {
		check     checkName
		code      int
		output    string
		wantError bool
	}{
		{checkVet, 1, "copylocks: copies lock value", false},
		{checkWorkflow, 1, "unknown job", false},
		{checkVuln, 3, "reachable vulnerability", false},
		{checkVuln, 1, "database unavailable", true},
		{checkWorkflow, 3, "configuration error", true},
		{checkVet, 1, "", true},
		{checkVet, 137, "killed", true},
	} {
		findings, err := commandFindings(tc.check, "app", tc.code, "", tc.output)
		if (err != nil) != tc.wantError {
			t.Fatalf("%s exit %d: findings=%v error=%v", tc.check, tc.code, findings, err)
		}
		if !tc.wantError && (len(findings) != 1 || findings[0].Message != tc.output) {
			t.Fatalf("lost tool diagnostic: %v", findings)
		}
	}
}

func TestStaticcheckWildcardDoesNotAcceptCompilerErrors(t *testing.T) {
	for _, code := range []string{"SA4006", "compile"} {
		output := `{"code":"` + code + `","message":"finding","location":{"file":"/src/a.go","line":1}}`
		_, err := parseFindings(1, output, "", []string{"SA*"})
		if (err == nil) != (code == "SA4006") {
			t.Fatalf("unexpected result for %s: %v", code, err)
		}
	}
}
