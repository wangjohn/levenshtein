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

func TestCheckSelectionMatchesTheLinter(t *testing.T) {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		checks []string
		code   string
		want   bool
	}{
		{"all selects an upstream analyzer", []string{"all"}, "nilness", true},
		{"all selects a house rule", []string{"all"}, "LV1003", true},
		{"a negation wins over all", []string{"all", "-ST1003"}, "ST1003", false},
		{"a later negation turns a glob's code off", []string{"SA*", "-SA1019"}, "SA1019", false},
		{"a later glob turns a negated code back on", []string{"-SA1019", "SA*"}, "SA1019", true},
		{"a later all turns a negated code back on", []string{"-SA5001", "all"}, "SA5001", true},
		{"a later negation turns all's code off", []string{"all", "-SA5001"}, "SA5001", false},
		{"a glob selects its family", []string{"SA*"}, "SA5001", true},
		{"a glob leaves other families alone", []string{"SA*"}, "S1002", false},
		{"a letter glob matches its category only", []string{"S*"}, "SA5001", false},
		{"a letter glob matches its own category", []string{"S*"}, "S1002", true},
		{"a letter glob does not prefix-match a bare analyzer", []string{"e*"}, "errcheck", false},
		{"a glob after a digit is a prefix", []string{"SA5*"}, "SA5001", true},
		{"names ignore case", []string{"sa5001"}, "SA5001", true},
		{"an exact name selects only itself", []string{"bodyclose"}, "bodyclose", true},
		{"an exact name excludes the rest", []string{"bodyclose"}, "sqlclosecheck", false},
		{"an empty selection reports nothing", nil, "SA5001", false},
		{"the shipped default keeps the deselected style rules off", tools.Checks, "ST1000", false},
		{"the shipped default keeps everything else on", tools.Checks, "errcheck", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := allowed(tc.checks, tc.code); got != tc.want {
				t.Fatalf("allowed(%v, %q) = %v, want %v", tc.checks, tc.code, got, tc.want)
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
