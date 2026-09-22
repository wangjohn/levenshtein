package main

import (
	"strings"
	"testing"
)

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

// This mirrors internal/verify's go-mod test: both commands exit 1 for a
// mismatch and for a failure alike, and only a mismatch is a finding.
func TestModFindingsSeparateMismatchesFromToolErrors(t *testing.T) {
	for _, tc := range []struct {
		step      modStep
		code      int
		stdout    string
		stderr    string
		wantError bool
	}{
		{modTidy, 1, "diff current/go.mod tidy/go.mod\n-require example.com/unused v0.0.0", "", false},
		{modTidy, 1, "", "verifying example.com/dep@v1.0.0: checksum mismatch\n\nSECURITY ERROR", false},
		{modVerify, 1, "", "example.com/dep v1.0.0: dir has been modified (/go/pkg/mod/example.com/dep@v1.0.0)", false},
		{modTidy, 1, "", `go: example.com/app imports example.invalid/dep: unrecognized import path "example.invalid/dep"`, true},
		{modVerify, 1, "", `go: example.invalid/dep@v1.0.0: unrecognized import path "example.invalid/dep"`, true},
		{modTidy, 137, "diff current/go.mod tidy/go.mod", "", true},
		{modVerify, 1, "", "", true},
	} {
		findings, err := modFindings(tc.step, "app", tc.code, tc.stdout, tc.stderr)
		if (err != nil) != tc.wantError {
			t.Fatalf("%s exit %d: findings=%v error=%v", tc.step, tc.code, findings, err)
		}
		if !tc.wantError && (len(findings) != 1 || findings[0].Code != string(checkMod) || findings[0].Message != strings.TrimSpace(tc.stdout+"\n"+tc.stderr)) {
			t.Fatalf("lost the go command's output: %v", findings)
		}
	}
	if findings, err := modFindings(modVerify, "app", 0, "all modules verified", ""); err != nil || len(findings) != 0 {
		t.Fatalf("a clean run is not a finding: %v %v", findings, err)
	}
}

// A direct Dagger call without a nonce could be answered with a stale verdict,
// so the checks whose state no source input covers refuse it before running.
func TestFreshChecksRequireANonce(t *testing.T) {
	for _, check := range []checkName{checkVuln, checkMod} {
		err := (&Levenshtein{}).SharedCheck(t.Context(), nil, string(check), ".", "")
		if err == nil || !strings.Contains(err.Error(), string(check)+" requires a unique nonce") {
			t.Fatalf("%s ran without a nonce: %v", check, err)
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
