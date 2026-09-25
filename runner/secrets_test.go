package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// This mirrors internal/verify's test: a finding never carries the secret,
// even from a report that includes it, and only the leak exit with leaks is a
// finding.
func TestSecretsFindingsNeverCarryTheSecret(t *testing.T) {
	report := `[{"RuleID":"generic-api-key","Description":"Detected a Generic API Key.","StartLine":9,"StartColumn":2,"Line":"api_key = \"` + secretsLeakyValue + `\"","Match":"api_key = \"` + secretsLeakyValue + `\"","Secret":"` + secretsLeakyValue + `","File":"settings.py","Fingerprint":"settings.py:generic-api-key:9"}]`

	findings, err := secretsFindings(secretsLeakExit, []byte(report), "")
	if err != nil || len(findings) != 1 || findings[0].Code != "generic-api-key" || findings[0].Location != (location{File: "settings.py", Line: 9, Column: 2}) {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
	encoded, err := json.Marshal(findings)
	if err != nil || strings.Contains(string(encoded), secretsLeakyValue) {
		t.Fatalf("a finding carried the secret: %s %v", encoded, err)
	}
}

func TestSecretsFindingsSeparateFindingsFromToolErrors(t *testing.T) {
	if findings, err := secretsFindings(0, []byte("[]"), ""); err != nil || len(findings) != 0 {
		t.Fatalf("a clean scan is not a finding: %v %v", findings, err)
	}

	leak := []byte(`[{"RuleID":"aws-access-token","Description":"AWS","File":"a.py","StartLine":3,"StartColumn":1,"Fingerprint":"a.py:aws-access-token:3"}]`)
	for _, tc := range []struct {
		code   int
		report []byte
	}{
		{1, nil},
		{2, leak},
		{secretsLeakExit, []byte("[]")},
		{0, leak},
		{secretsLeakExit, nil},
	} {
		if findings, err := secretsFindings(tc.code, tc.report, "fatal"); err == nil {
			t.Errorf("exit %d with report %q must be an error, got %v", tc.code, tc.report, findings)
		}
	}
}
