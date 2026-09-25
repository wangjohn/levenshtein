package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"dagger/levenshtein/internal/checktool"
)

// The shipped selection keeps its opt-in and deselected rules off and the rest
// on, and a check can still add an opt-in rule. checktool's own tests cover
// the filter's general cases.
func TestShippedSelectionLeavesOptInRulesOff(t *testing.T) {
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
		{"the shipped default keeps the deselected style rules off", tools.Checks, "ST1000", false},
		{"the shipped default keeps gocognit off", tools.Checks, "gocognit", false},
		{"adding gocognit to the shipped default turns it on", append(slices.Clone(tools.Checks), "gocognit"), "gocognit", true},
		{"the shipped default keeps deferInLoop off", tools.Checks, "deferInLoop", false},
		{"adding deferInLoop to the shipped default turns it on", append(slices.Clone(tools.Checks), "deferInLoop"), "deferInLoop", true},
		{"the shipped default keeps everything else on", tools.Checks, "errcheck", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := checktool.Allowed(tc.checks, tc.code); got != tc.want {
				t.Fatalf("Allowed(%v, %q) = %v, want %v", tc.checks, tc.code, got, tc.want)
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
		if !checktool.Allowed(tools.Checks, code) {
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

// An added pattern is joined into one -checks flag, so anything but a single
// pattern is refused before the linter runs. The CLI validates levenshtein.json
// with a copy of the same expression.
func TestAddedCheckPatternSyntax(t *testing.T) {
	for _, check := range []string{"gocognit", "-unparam", "SA5*", "S*", "all", "*", "-ST1000", "appendAssign"} {
		if !lintPattern.MatchString(check) {
			t.Errorf("rejected %q", check)
		}
	}
	for _, check := range []string{"", "-", "--unparam", "gocognit,unparam", "SA 5001", " gocognit", "SA*5", "**", "go-cognit", "1000"} {
		if lintPattern.MatchString(check) {
			t.Errorf("accepted %q", check)
		}
	}
}
