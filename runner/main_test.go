package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestLintExitStatusAndDiagnosticsAgree(t *testing.T) {
	valid := `{"code":"SA5001","message":"defer before error check","location":{"file":"/src/close.go","line":7,"column":2}}`
	findings, err := parseFindings(1, valid, "", []string{"SA5001"})
	if err != nil || len(findings) != 1 || findings[0].Location.File != "close.go" {
		t.Fatalf("lost lint diagnostic: %v, %v", findings, err)
	}
	for _, tc := range []struct {
		code   int
		out    string
		stderr string
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

// selectionCase is one row of testdata/selection.json.
type selectionCase struct {
	Name   string   `json:"name"`
	Checks []string `json:"checks"`
	Code   string   `json:"code"`
	Want   bool     `json:"want"`
}

func TestCheckSelectionMatchesTheLinter(t *testing.T) {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		t.Fatal(err)
	}

	// The general cases live in a table the standalone CLI's copy of this
	// filter (internal/verify/findings.go) also loads, so the two cannot drift.
	data, err := os.ReadFile("testdata/selection.json")
	if err != nil {
		t.Fatal(err)
	}
	var shared struct {
		Cases []selectionCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &shared); err != nil {
		t.Fatal(err)
	}
	if len(shared.Cases) == 0 {
		t.Fatal("testdata/selection.json has no cases")
	}

	shared.Cases = append(shared.Cases,
		selectionCase{Name: "the shipped default keeps the deselected style rules off", Checks: tools.Checks, Code: "ST1000", Want: false},
		selectionCase{Name: "the shipped default keeps everything else on", Checks: tools.Checks, Code: "errcheck", Want: true},
	)
	for _, tc := range shared.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			if got := allowed(tc.Checks, tc.Code); got != tc.Want {
				t.Fatalf("allowed(%v, %q) = %v, want %v", tc.Checks, tc.Code, got, tc.Want)
			}
		})
	}
}

func TestSelfTestCoversEveryDefaultRule(t *testing.T) {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		t.Fatal(err)
	}

	for _, code := range expectedBadCodes {
		if !allowed(tools.Checks, code) {
			t.Errorf("the bad fixture expects %s, which the default selection does not report", code)
		}
	}

	script, err := os.ReadFile("../scripts/test-checks")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, found := strings.Cut(string(script), "\nexpected='")
	block, _, closed := strings.Cut(rest, "'")
	if !found || !closed {
		t.Fatal("scripts/test-checks must define the bad-fixture codes as expected='...'")
	}
	if got := strings.Fields(block); !slices.Equal(got, expectedBadCodes) {
		t.Errorf("scripts/test-checks expects %v, runner expects %v", got, expectedBadCodes)
	}
}

func TestLinterDependencyMatchesToolchain(t *testing.T) {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		t.Fatal(err)
	}
	module, err := os.ReadFile("lint/go.mod")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(module), "honnef.co/go/tools "+tools.Staticcheck+"\n") {
		t.Fatal("lint module and toolchain.json must pin the same Staticcheck version")
	}
}
