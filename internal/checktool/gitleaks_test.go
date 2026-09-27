package checktool

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// fakeSecret stands in for a leaked value in these tests. It is made up.
const fakeSecret = "9f8a7Qm2Lx0Zc4Vb6Nn1Ty8Ru3Ew5Qd" // gitleaks:allow

// A finding carries the rule, the location, and the fingerprint to allowlist,
// and nothing that could hold the secret, even when a report were to include
// it unredacted.
func TestSecretsFindingsNeverCarryTheSecret(t *testing.T) {
	report := `[{"RuleID":"generic-api-key","Description":"Detected a Generic API Key, potentially exposing access to various services and sensitive operations.","StartLine":9,"EndLine":9,"StartColumn":2,"EndColumn":44,"Line":"api_key = \"` + fakeSecret + `\"","Match":"api_key = \"` + fakeSecret + `\"","Secret":"` + fakeSecret + `","File":"settings.py","SymlinkFile":"","Commit":"","Entropy":4.8,"Author":"","Email":"","Date":"","Message":"","Tags":[],"Fingerprint":"settings.py:generic-api-key:9"}]`

	findings, err := SecretsFindings(Run{ExitCode: SecretsLeakExit, Stderr: "WRN leaks found: 1"}, []byte(report))
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Code != "generic-api-key" || findings[0].Location != (Location{File: "settings.py", Line: 9, Column: 2}) {
		t.Fatalf("unexpected findings: %+v", findings)
	}
	if !strings.Contains(findings[0].Message, "add settings.py:generic-api-key:9 to .gitleaksignore") {
		t.Fatalf("the message must say how to allowlist a false positive: %q", findings[0].Message)
	}

	encoded, err := json.Marshal(findings)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), fakeSecret) {
		t.Fatalf("a finding carried the secret: %s", encoded)
	}
}

// Only the leak exit with a report that lists leaks is a finding; every other
// outcome is a tool error, never a pass.
func TestSecretsFindingsSeparateFindingsFromToolErrors(t *testing.T) {
	if findings, err := SecretsFindings(Run{}, []byte("[]")); err != nil || len(findings) != 0 {
		t.Fatalf("a clean scan is not a finding: %v %v", findings, err)
	}

	// A one-line key file puts its secret on line 1.
	firstLine := []byte(`[{"RuleID":"private-key","Description":"Key","File":"id_ed25519","StartLine":1,"StartColumn":1,"Fingerprint":"id_ed25519:private-key:1"}]`)
	if findings, err := SecretsFindings(Run{ExitCode: SecretsLeakExit}, firstLine); err != nil || len(findings) != 1 || findings[0].Location.Line != 1 {
		t.Fatalf("a secret on the first line is a finding there: %v %v", findings, err)
	}

	leak := []byte(`[{"RuleID":"aws-access-token","Description":"AWS","File":"a.py","StartLine":3,"StartColumn":1,"Fingerprint":"a.py:aws-access-token:3"}]`)
	for _, tc := range []struct {
		code   int
		report []byte
	}{
		{1, nil},
		{1, []byte("[]")},
		{2, leak},
		{SecretsLeakExit, []byte("[]")},
		{0, leak},
		{SecretsLeakExit, nil},
		{0, []byte("not json")},
		{SecretsLeakExit, []byte(`[{"RuleID":"","File":"a.py","StartLine":3}]`)},
		{SecretsLeakExit, []byte(`[{"RuleID":"aws-access-token","File":"a.py","StartLine":0}]`)},
	} {
		if findings, err := SecretsFindings(Run{ExitCode: tc.code, Stderr: "fatal"}, tc.report); err == nil {
			t.Errorf("exit %d with report %q must be an error, got %v", tc.code, tc.report, findings)
		}
	}
}

func TestSecretsArgumentsRedactAndNameTheConfiguration(t *testing.T) {
	args := SecretsArguments("gitleaks", false, "/tmp/report.json")
	for _, want := range []string{"dir", ".", "--redact=100", "--exit-code=3", "--report-format=json", "--report-path=/tmp/report.json"} {
		if !slices.Contains(args, want) {
			t.Errorf("arguments %q lack %q", args, want)
		}
	}
	if args[0] != "gitleaks" || slices.ContainsFunc(args, func(arg string) bool { return strings.HasPrefix(arg, "--config") }) {
		t.Fatalf("without a root .gitleaks.toml gitleaks must use its default rules: %q", args)
	}
	if args := SecretsArguments("gitleaks", true, "/tmp/report.json"); !slices.Contains(args, "--config=.gitleaks.toml") {
		t.Fatalf("a root .gitleaks.toml must be named: %q", args)
	}
}
