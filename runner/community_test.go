package main

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestCommunityPatternsMatchTheSharedTable(t *testing.T) {
	data, err := os.ReadFile("testdata/community-patterns.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases struct {
		Syntax []struct {
			Pattern string `json:"pattern"`
			Valid   bool   `json:"valid"`
		} `json:"syntax"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}

	for _, test := range cases.Syntax {
		if got := communityPattern.MatchString(test.Pattern); got != test.Valid {
			t.Errorf("communityPattern.MatchString(%q) = %v, want %v", test.Pattern, got, test.Valid)
		}
	}
}

func TestCoreURLsMatchTheSharedTable(t *testing.T) {
	data, err := os.ReadFile("testdata/core-urls.json")
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []struct {
			Code string `json:"code"`
			URL  string `json:"url"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}

	for _, test := range table.Cases {
		if got := coreURL(test.Code); got != test.URL {
			t.Errorf("coreURL(%q) = %q, want %q", test.Code, got, test.URL)
		}
	}
}

var errsModules = []ruleModule{{Path: "example.com/lvrules-errors", Version: "v1.4.0", Namespace: "errs", Select: []string{"errs_*"}}}

func TestChecksSplitBetweenTheLinters(t *testing.T) {
	core, community, err := splitChecks([]string{"gocognit", "-SA5*", "errs_nopanic", "-errs_wrapf"}, errsModules)
	if err != nil {
		t.Fatal(err)
	}

	if !slices.Equal(core, []string{"gocognit", "-SA5*"}) || !slices.Equal(community, []string{"errs_nopanic", "-errs_wrapf"}) {
		t.Errorf("core %v, community %v", core, community)
	}
	for _, bad := range []string{"other_*", "errs_no-panic"} {
		if _, _, err := splitChecks([]string{bad}, errsModules); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestRuleModulesArgumentIsTheCLIsPlan(t *testing.T) {
	modules, err := parseRuleModules(`[{"path": "example.com/lvrules-errors", "version": "v1.4.0", "namespace": "errs", "select": ["errs_*"], "advisory": ["errs_sentinel"], "settings": {"errs_nopanic": {"allow": "Must"}}}]`)
	if err != nil || len(modules) != 1 || modules[0].Settings["errs_nopanic"]["allow"] != "Must" {
		t.Fatalf("modules %+v, error %v", modules, err)
	}

	for _, bad := range []string{
		`[{"path": "example.com/m", "version": "v1.0.0", "namespace": "m", "select": ["m_*"], "dir": "/etc"}]`,
		`[{"path": "example.com/m", "version": "v1.0.0", "namespace": "m", "select": []}]`,
		`{}`,
	} {
		if _, err := parseRuleModules(bad); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if modules, err := parseRuleModules(""); modules != nil || err != nil {
		t.Errorf("no argument means no modules: %v %v", modules, err)
	}
}

const sampleReport = `{"rules": [
	{"code": "errs_nopanic", "source": "example.com/lvrules-errors@v1.4.0", "url": "https://example.com/nopanic", "advisory": false},
	{"code": "errs_sentinel", "source": "example.com/lvrules-errors@v1.4.0", "url": "https://example.com/sentinel", "advisory": true},
	{"code": "lvrules_mixed", "url": "https://example.com/directives", "advisory": false}],
	"warnings": [{"kind": "rule-renamed", "message": "old name"}]}`

func communityLine(code, severity string, line int) string {
	return fmt.Sprintf(`{"code":%q,"severity":%q,"location":{"file":"/src/app/store.go","line":%d,"column":2},"message":"m"}`, code, severity, line)
}

func TestCommunityOutputBecomesFindings(t *testing.T) {
	stdout := strings.Join([]string{
		communityLine("errs_nopanic", "error", 7),
		communityLine("errs_sentinel", "warning", 9),
		communityLine("staticcheck", "warning", 11),
	}, "\n")

	findings, warnings, err := parseCommunity(communityRun{ExitCode: 1, Stdout: stdout, Report: sampleReport, Reported: true})
	if err != nil {
		t.Fatal(err)
	}

	want := []diagnostic{
		{Code: "errs_nopanic", Message: "m", Location: location{File: "app/store.go", Line: 7, Column: 2}, Source: "example.com/lvrules-errors@v1.4.0", URL: "https://example.com/nopanic"},
		{Code: "errs_sentinel", Message: "m", Location: location{File: "app/store.go", Line: 9, Column: 2}, Source: "example.com/lvrules-errors@v1.4.0", URL: "https://example.com/sentinel", Advisory: true},
		{Code: "staticcheck", Message: "m", Location: location{File: "app/store.go", Line: 11, Column: 2}, Advisory: true},
	}
	if !slices.Equal(findings, want) {
		t.Errorf("findings = %+v\nwant %+v", findings, want)
	}
	if len(warnings) != 1 || warnings[0].Message != "old name" {
		t.Errorf("warnings = %+v", warnings)
	}
}

func TestCommunityOutputThatDisagreesWithItselfIsAnError(t *testing.T) {
	failing := communityLine("errs_nopanic", "error", 7)
	advisory := communityLine("errs_sentinel", "warning", 9)
	failedReport := `{"rules": [], "failures": [{"code": "errs_nopanic", "source": "example.com/lvrules-errors@v1.4.0", "package": "example.com/app/store", "error": "panic: boom"}]}`

	for _, test := range []struct {
		name    string
		run     communityRun
		message string
	}{
		{"no report", communityRun{ExitCode: 0}, "without finishing"},
		{"failed rule", communityRun{ExitCode: 4, Report: failedReport, Reported: true}, "errs_nopanic (example.com/lvrules-errors@v1.4.0) failed on example.com/app/store: panic: boom"},
		{"crash", communityRun{ExitCode: 2, Report: sampleReport, Reported: true, Stderr: "fatal"}, "exited 2"},
		{"stderr", communityRun{ExitCode: 0, Report: sampleReport, Reported: true, Stderr: "warning: skipped package"}, "clean result"},
		{"unselected code", communityRun{ExitCode: 1, Stdout: communityLine("errs_wrapf", "error", 1), Report: sampleReport, Reported: true}, "did not select"},
		{"advisory mismatch", communityRun{ExitCode: 1, Stdout: communityLine("errs_sentinel", "error", 1), Report: sampleReport, Reported: true}, "advisory setting"},
		{"exit 0 with a failing finding", communityRun{ExitCode: 0, Stdout: failing, Report: sampleReport, Reported: true}, "does not match"},
		{"exit 1 with only advisory findings", communityRun{ExitCode: 1, Stdout: advisory, Report: sampleReport, Reported: true}, "does not match"},
		{"not JSON", communityRun{ExitCode: 1, Stdout: "store.go:1: text", Report: sampleReport, Reported: true}, "invalid community linter JSON"},
		{"compile error", communityRun{ExitCode: 1, Stdout: communityLine("compile", "error", 3), Report: sampleReport, Reported: true}, "could not type-check"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := parseCommunity(test.run)

			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Errorf("got %v, want an error containing %q", err, test.message)
			}
		})
	}
}

func TestMixedDirectivesAreReportedOnce(t *testing.T) {
	stale := func(line int) diagnostic {
		return diagnostic{Code: staticcheckCode, Message: staleDirective, Location: location{File: "a.go", Line: line, Column: 2}}
	}
	core := []diagnostic{stale(4), stale(9), {Code: "SA4006", Message: "m", Location: location{File: "a.go", Line: 12}}}
	community := []diagnostic{{Code: codeMixed, Message: "mixed", Location: location{File: "a.go", Line: 4, Column: 2}}, stale(4)}

	var got []string
	for _, finding := range mergeFindings(core, community) {
		got = append(got, fmt.Sprintf("%s:%d", finding.Code, finding.Location.Line))
	}

	want := []string{"staticcheck:9", "SA4006:12", "lvrules_mixed:4"}
	if !slices.Equal(got, want) {
		t.Errorf("merged %v, want %v", got, want)
	}
}

func TestOnlyFailingFindingsFailTheCheck(t *testing.T) {
	advisory := lintOutcome{Findings: []diagnostic{{Code: "errs_sentinel", Advisory: true}}, Warnings: []warning{{Kind: warningRuleModuleRetracted, Message: "retracted"}}}
	if advisory.failing() {
		t.Error("an advisory finding must not fail the check")
	}
	report, err := advisory.report()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(report, `"advisory":true`) || !strings.Contains(report, `"rule-module-retracted"`) {
		t.Errorf("report = %s", report)
	}
	if !(lintOutcome{Findings: []diagnostic{{Code: "SA4006"}}}).failing() {
		t.Error("a core finding fails the check")
	}
}

func TestTheShippedRuleModuleListIsReadable(t *testing.T) {
	warnings, err := releaseNotices(errsModules)
	if err != nil || warnings != nil {
		t.Errorf("the shipped list deprecates nothing yet: %v %v", warnings, err)
	}
}

func TestVendoringFollowsTheWorkspace(t *testing.T) {
	for _, test := range []struct {
		name   string
		files  []string
		module string
		want   string
	}{
		{"a module on its own", nil, "app", "app/vendor/modules.txt"},
		{"the repository root", nil, ".", "vendor/modules.txt"},
		{"a workspace at the root", []string{"go.work"}, "services/app", "vendor/modules.txt"},
		{"a workspace between", []string{"services/go.work"}, "services/app", "services/vendor/modules.txt"},
		{"a module holding the workspace", []string{"services/app/go.work", "go.work"}, "services/app", "services/app/vendor/modules.txt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			exists := func(file string) (bool, error) { return slices.Contains(test.files, file), nil }

			got, err := vendorFile(test.module, exists)

			if err != nil || got != test.want {
				t.Errorf("vendorFile(%q) = %q, %v; want %q", test.module, got, err, test.want)
			}
		})
	}
}
