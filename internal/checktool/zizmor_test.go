package checktool

import (
	"slices"
	"strings"
	"testing"
)

// Only zizmor's medium and high finding codes are findings, and every other
// nonzero exit is a tool error, never a pass.
func TestZizmorFindingsSeparateFindingsFromToolErrors(t *testing.T) {
	for _, tc := range []struct {
		code      int
		stdout    string
		stderr    string
		wantError bool
	}{
		{code: 14, stdout: "error[template-injection]: code injection via template expansion"},
		{code: 13, stdout: "warning[excessive-permissions]: overly broad permissions"},
		{code: 1, stderr: "fatal: no audit was performed", wantError: true},
		{code: 2, stderr: "error: unexpected argument '--bogus' found", wantError: true},
		{code: 3, stderr: "fatal: no inputs collected", wantError: true},
		{code: 12, stdout: "help[self-repository]: use GitHub's dedicated self-repository syntax", wantError: true},
		{code: 11, stdout: "info[template-injection]: code injection via template expansion", wantError: true},
		{code: 14, wantError: true},
		{code: 137, stderr: "killed", wantError: true},
	} {
		findings, err := ZizmorFindings(".", Run{ExitCode: tc.code, Stdout: tc.stdout, Stderr: tc.stderr})
		if (err != nil) != tc.wantError {
			t.Fatalf("exit %d: findings=%v error=%v", tc.code, findings, err)
		}
		if !tc.wantError && (len(findings) != 1 || findings[0].Code != string(KindWorkflowSecurity) || findings[0].Message != strings.TrimSpace(tc.stdout+"\n"+tc.stderr) || findings[0].Location.File != ".") {
			t.Fatalf("lost zizmor's report: %v", findings)
		}
	}

	if findings, err := ZizmorFindings(".", Run{Stdout: "No findings to report. Good job!"}); err != nil || len(findings) != 0 {
		t.Fatalf("a clean run is not a finding: %v %v", findings, err)
	}
}

func TestZizmorArgumentsAuditOfflineWithOneConfiguration(t *testing.T) {
	inputs := []string{".github/workflows/ci.yml", "action.yml"}
	common := []string{"zizmor", "--offline", "--strict-collection", "--min-severity=medium", "--format=plain", "--color=never", "--no-progress", "--quiet"}

	if got, want := ZizmorArguments("zizmor", "", inputs), append(slices.Clone(common), "--no-config", ".github/workflows/ci.yml", "action.yml"); !slices.Equal(got, want) {
		t.Fatalf("without a configuration: %q, want %q", got, want)
	}
	if got, want := ZizmorArguments("zizmor", ".github/zizmor.yml", inputs), append(slices.Clone(common), "--config=.github/zizmor.yml", ".github/workflows/ci.yml", "action.yml"); !slices.Equal(got, want) {
		t.Fatalf("with a configuration: %q, want %q", got, want)
	}
}
