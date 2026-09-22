package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// This mirrors internal/verify's workflow-security test: only zizmor's medium
// and high finding codes are findings, and every other nonzero exit is a tool
// error, never a pass.
func TestZizmorFindingsSeparateFindingsFromToolErrors(t *testing.T) {
	for _, tc := range []struct {
		code      int
		stdout    string
		stderr    string
		wantError bool
	}{
		{14, "error[template-injection]: code injection via template expansion", "", false},
		{13, "warning[excessive-permissions]: overly broad permissions", "", false},
		{1, "", "fatal: no audit was performed", true},
		{2, "", "error: unexpected argument '--bogus' found", true},
		{3, "", "fatal: no inputs collected", true},
		{12, "help[self-repository]: use GitHub's dedicated self-repository syntax", "", true},
		{11, "info[template-injection]: code injection via template expansion", "", true},
		{14, "", "", true},
		{137, "", "killed", true},
	} {
		findings, err := zizmorFindings(".", tc.code, tc.stdout, tc.stderr)
		if (err != nil) != tc.wantError {
			t.Fatalf("exit %d: findings=%v error=%v", tc.code, findings, err)
		}
		if !tc.wantError && (len(findings) != 1 || findings[0].Code != string(checkWorkflowSecurity) || findings[0].Message != strings.TrimSpace(tc.stdout+"\n"+tc.stderr)) {
			t.Fatalf("lost zizmor's report: %v", findings)
		}
	}
	if findings, err := zizmorFindings(".", 0, "No findings to report. Good job!", ""); err != nil || len(findings) != 0 {
		t.Fatalf("a clean run is not a finding: %v %v", findings, err)
	}
}

func TestZizmorArgumentsAuditOfflineWithOneConfiguration(t *testing.T) {
	inputs := []string{".github/workflows/ci.yml", "action.yml"}
	common := []string{"zizmor", "--offline", "--strict-collection", "--min-severity=medium", "--format=plain", "--color=never", "--no-progress", "--quiet"}

	if got, want := zizmorArguments("zizmor", "", inputs), append(slices.Clone(common), "--no-config", ".github/workflows/ci.yml", "action.yml"); !slices.Equal(got, want) {
		t.Fatalf("without a configuration: %q, want %q", got, want)
	}
	if got, want := zizmorArguments("zizmor", ".github/zizmor.yml", inputs), append(slices.Clone(common), "--config=.github/zizmor.yml", ".github/workflows/ci.yml", "action.yml"); !slices.Equal(got, want) {
		t.Fatalf("with a configuration: %q, want %q", got, want)
	}
}

// Every platform either executor runs on has a reviewed archive, and the
// engine's CPU variant does not hide the one for its architecture.
func TestZizmorPinCoversEveryPlatform(t *testing.T) {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		t.Fatal(err)
	}
	if tools.Zizmor.Version == "" {
		t.Fatal("toolchain.json must pin a zizmor version")
	}
	for _, platform := range []string{"linux/amd64", "linux/arm64", "linux/arm64/v8", "darwin/amd64", "darwin/arm64"} {
		if _, err := tools.Zizmor.archive(platform); err != nil {
			t.Errorf("%s: %v", platform, err)
		}
	}
	if _, err := tools.Zizmor.archive("windows/amd64"); err == nil {
		t.Error("an unpinned platform must be an error, not an unverified download")
	}
}
