package main

import "testing"

func TestCommandFailuresDoNotBecomePassingResults(t *testing.T) {
	for _, tc := range []struct {
		check     string
		code      int
		output    string
		wantError bool
	}{
		{"go-vet", 1, "copylocks: copies lock value", false},
		{"workflow-lint", 1, "unknown job", false},
		{"go-vuln", 3, "reachable vulnerability", false},
		{"go-vuln", 1, "database unavailable", true},
		{"workflow-lint", 3, "configuration error", true},
		{"go-vet", 1, "", true},
		{"go-vet", 137, "killed", true},
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

func TestNamedChecksComposeInCustomRuns(t *testing.T) {
	cfg, err := parseConfig(`{"modules":["."],"runs":{"service":["go-lint","go-vet","go-http","go-sql","workflow-lint","go-vuln"]}}`)
	if err != nil {
		t.Fatal(err)
	}
	checks, err := cfg.selectChecks("service")
	if err != nil || len(checks) != 6 {
		t.Fatalf("checks=%v error=%v", checks, err)
	}
}
