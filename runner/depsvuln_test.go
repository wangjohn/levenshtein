package main

import (
	"slices"
	"strings"
	"testing"
)

// This mirrors internal/verify's test: one finding per vulnerable package
// version, located at the lockfile relative to the scanned directory.
func TestDepsFindingsReportOnePackageVersionPerFinding(t *testing.T) {
	report := `{"results":[{"source":{"path":"/src/web/package-lock.json","type":"lockfile"},"packages":[
		{"package":{"name":"lodash","version":"4.17.20","ecosystem":"npm"},"groups":[{"ids":["GHSA-35jh-r3h4-6jhm","GHSA-r5fr-rjxr-66jc"],"aliases":["CVE-2021-23337","GHSA-35jh-r3h4-6jhm","GHSA-r5fr-rjxr-66jc"],"max_severity":"8.1"}],"vulnerabilities":[{"id":"GHSA-35jh-r3h4-6jhm","summary":"Command Injection in lodash"},{"id":"GHSA-r5fr-rjxr-66jc"}]},
		{"package":{"name":"minimist","version":"1.2.5","ecosystem":"npm"},"vulnerabilities":[{"id":"GHSA-xvch-5gv4-984h","summary":"Prototype Pollution in minimist"}]}]}]}`

	findings, err := depsFindings(depsVulnerableExit, []byte(report), "")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, finding := range findings {
		got = append(got, finding.Location.File+": "+finding.Message)
	}
	want := []string{
		"web/package-lock.json: lodash@4.17.20 (npm) has known vulnerabilities: GHSA-35jh-r3h4-6jhm, GHSA-r5fr-rjxr-66jc (CVE-2021-23337, severity 8.1) Command Injection in lodash",
		"web/package-lock.json: minimist@1.2.5 (npm) has known vulnerabilities: GHSA-xvch-5gv4-984h Prototype Pollution in minimist",
	}
	if !slices.Equal(got, want) || findings[0].Code != string(checkDepsVuln) {
		t.Fatalf("findings:\n%q\nwant:\n%q", got, want)
	}
}

func TestDepsFindingsSeparateFindingsFromToolErrors(t *testing.T) {
	if findings, err := depsFindings(0, []byte(`{"results":[]}`), ""); err != nil || len(findings) != 0 {
		t.Fatalf("a clean scan is not a finding: %v %v", findings, err)
	}
	if _, err := depsFindings(depsNoPackagesExit, nil, ""); err == nil || !strings.Contains(err.Error(), "found no supported dependency lockfile") {
		t.Fatalf("no lockfile must be an error that says so: %v", err)
	}
	for _, tc := range []struct {
		code   int
		report string
	}{
		{127, ""},
		{depsVulnerableExit, `{"results":[]}`},
		{depsVulnerableExit, ""},
	} {
		if findings, err := depsFindings(tc.code, []byte(tc.report), "query failed"); err == nil {
			t.Errorf("exit %d with report %q must be an error, got %v", tc.code, tc.report, findings)
		}
	}
}

// This mirrors internal/verify's test: an advisory osv-scanner does not count
// toward its exit code, such as one Debian rates unimportant, is not reported.
func TestDepsFindingsLeaveOutWhatOSVScannerDoesNotCount(t *testing.T) {
	unimportant := `{"ids":["DEBIAN-CVE-2016-2781"],"experimental_analysis":{"DEBIAN-CVE-2016-2781":{"called":true,"unimportant":true}}}`
	report := func(groups ...string) []byte {
		return []byte(`{"results":[{"source":{"path":"/src/bom.cdx.json","type":"sbom"},"packages":[{"package":{"name":"coreutils","version":"9.7-3","ecosystem":"Debian:13"},"groups":[` + strings.Join(groups, ",") + `],"vulnerabilities":[{"id":"DEBIAN-CVE-2016-2781"},{"id":"DEBIAN-CVE-2025-5278","summary":"heap overflow in sort"}]}]}]}`)
	}

	findings, err := depsFindings(0, report(unimportant), "")
	if err != nil || len(findings) != 0 {
		t.Fatalf("only unimportant advisories must pass as osv-scanner's exit 0 says: %v %v", findings, err)
	}

	findings, err = depsFindings(depsVulnerableExit, report(unimportant, `{"ids":["DEBIAN-CVE-2025-5278"]}`), "")
	if err != nil {
		t.Fatal(err)
	}
	want := "coreutils@9.7-3 (Debian:13) has known vulnerabilities: DEBIAN-CVE-2025-5278 heap overflow in sort"
	if len(findings) != 1 || findings[0].Message != want || findings[0].Location.File != "bom.cdx.json" {
		t.Fatalf("findings: %+v\nwant one: %s", findings, want)
	}
}

func TestDepsArgumentsLeaveGoToGoVuln(t *testing.T) {
	args := depsArguments("osv-scanner", false, depsReportPath)
	for _, want := range []string{"--experimental-exclude=r:(^|/)(testdata|vendor|node_modules)(/|$)", "--experimental-disable-plugins=go/gomod", "--no-call-analysis=all", "--no-resolve", "--output-file=" + depsReportPath} {
		if !slices.Contains(args, want) {
			t.Errorf("arguments %q lack %q", args, want)
		}
	}
	if args := depsArguments("osv-scanner", true, depsReportPath); !slices.Contains(args, "--config=osv-scanner.toml") {
		t.Fatalf("a root osv-scanner.toml must be named: %q", args)
	}
}
