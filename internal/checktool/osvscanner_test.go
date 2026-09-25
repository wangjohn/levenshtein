package checktool

import (
	"slices"
	"strings"
	"testing"
)

// depsSampleReport is osv-scanner 2.6.0's JSON for deps-vulnerable, trimmed to
// the fields deps-vuln reads plus a few it ignores.
const depsSampleReport = `{
  "results": [
    {
      "source": {"path": "/scan/web/package-lock.json", "type": "lockfile"},
      "packages": [
        {
          "package": {"name": "lodash", "version": "4.17.20", "ecosystem": "npm"},
          "groups": [
            {"ids": ["GHSA-29mw-wpgm-hmr9"], "aliases": ["CVE-2020-28500", "GHSA-29mw-wpgm-hmr9"], "max_severity": "5.3"},
            {"ids": ["GHSA-35jh-r3h4-6jhm", "GHSA-r5fr-rjxr-66jc"], "aliases": ["CVE-2021-23337", "GHSA-35jh-r3h4-6jhm", "GHSA-r5fr-rjxr-66jc"], "max_severity": "8.1"}
          ],
          "vulnerabilities": [
            {"id": "GHSA-29mw-wpgm-hmr9", "summary": "Regular Expression Denial of Service (ReDoS) in lodash", "modified": "2025-01-01T00:00:00Z"},
            {"id": "GHSA-35jh-r3h4-6jhm", "summary": "Command Injection in lodash"},
            {"id": "GHSA-r5fr-rjxr-66jc", "summary": "lodash vulnerable to Code Injection via template imports key names"}
          ]
        },
        {
          "package": {"name": "minimist", "version": "1.2.5", "ecosystem": "npm"},
          "vulnerabilities": [{"id": "GHSA-xvch-5gv4-984h", "summary": "Prototype Pollution in minimist"}]
        }
      ]
    }
  ],
  "experimental_config": {"licenses": {"summary": false, "allowlist": null}}
}`

func TestDepsFindingsReportOnePackageVersionPerFinding(t *testing.T) {
	findings, err := DepsFindings("/scan", Run{ExitCode: DepsVulnerableExit}, []byte(depsSampleReport))
	if err != nil {
		t.Fatal(err)
	}

	want := []Finding{
		{
			Code:     string(KindDepsVuln),
			Message:  "lodash@4.17.20 (npm) has known vulnerabilities: GHSA-29mw-wpgm-hmr9 (CVE-2020-28500, severity 5.3) Regular Expression Denial of Service (ReDoS) in lodash; GHSA-35jh-r3h4-6jhm, GHSA-r5fr-rjxr-66jc (CVE-2021-23337, severity 8.1) Command Injection in lodash",
			Location: Location{File: "web/package-lock.json", Line: 1},
		},
		{
			Code:     string(KindDepsVuln),
			Message:  "minimist@1.2.5 (npm) has known vulnerabilities: GHSA-xvch-5gv4-984h Prototype Pollution in minimist",
			Location: Location{File: "web/package-lock.json", Line: 1},
		},
	}
	if !slices.Equal(findings, want) {
		t.Fatalf("findings:\n%+v\nwant:\n%+v", findings, want)
	}
}

// A lockfile is reported relative to the scanned directory however
// osv-scanner spelled its path.
func TestDepsFindingsLocateTheLockfileInTheScan(t *testing.T) {
	for path, want := range map[string]string{
		"/scan/web/package-lock.json":    "web/package-lock.json",
		"/scan/./package-lock.json":      "package-lock.json",
		"./package-lock.json":            "package-lock.json",
		"/elsewhere/package-lock.json":   "/elsewhere/package-lock.json",
		"/scanned/web/package-lock.json": "/scanned/web/package-lock.json",
	} {
		report := `{"results":[{"source":{"path":"` + path + `"},"packages":[{"package":{"name":"minimist","version":"1.2.5","ecosystem":"npm"},"vulnerabilities":[{"id":"GHSA-xvch-5gv4-984h"}]}]}]}`
		findings, err := DepsFindings("/scan", Run{ExitCode: DepsVulnerableExit}, []byte(report))
		if err != nil || len(findings) != 1 || findings[0].Location.File != want {
			t.Errorf("%s: findings=%+v error=%v, want it at %s", path, findings, err, want)
		}
	}
}

// A scan with nothing to scan, a failed advisory query, and a report that
// contradicts the exit code are all errors, never a pass.
func TestDepsFindingsSeparateFindingsFromToolErrors(t *testing.T) {
	if findings, err := DepsFindings("/scan", Run{}, []byte(`{"results":[]}`)); err != nil || len(findings) != 0 {
		t.Fatalf("a clean scan is not a finding: %v %v", findings, err)
	}
	if _, err := DepsFindings("/scan", Run{ExitCode: depsNoPackagesExit, Stderr: "No package sources found"}, nil); err == nil || !strings.Contains(err.Error(), "found no supported dependency lockfile") {
		t.Fatalf("no lockfile must be an error that says so: %v", err)
	}

	for _, tc := range []struct {
		code   int
		report string
	}{
		{127, ""},
		{127, `{"results":[]}`},
		{2, depsSampleReport},
		{0, depsSampleReport},
		{DepsVulnerableExit, `{"results":[]}`},
		{DepsVulnerableExit, ""},
		{0, "not json"},
	} {
		if findings, err := DepsFindings("/scan", Run{ExitCode: tc.code, Stderr: "query failed"}, []byte(tc.report)); err == nil {
			t.Errorf("exit %d with report %q must be an error, got %v", tc.code, tc.report, findings)
		}
	}
}

// osv-scanner lists a Debian advisory the distribution rates unimportant, but
// does not count it toward its exit code. deps-vuln must judge the same way:
// an SBOM whose only advisories are unimportant passes, and the rest of a
// package's advisories are reported without them.
func TestDepsFindingsLeaveOutWhatOSVScannerDoesNotCount(t *testing.T) {
	unimportant := `{"ids": ["DEBIAN-CVE-2016-2781"], "aliases": ["CVE-2016-2781"], "experimental_analysis": {"DEBIAN-CVE-2016-2781": {"called": true, "unimportant": true}}}`
	report := func(groups ...string) []byte {
		return []byte(`{"results": [{"source": {"path": "/scan/bom.cdx.json", "type": "sbom"}, "packages": [{"package": {"name": "coreutils", "version": "9.7-3", "ecosystem": "Debian:13"}, "groups": [` + strings.Join(groups, ",") + `], "vulnerabilities": [{"id": "DEBIAN-CVE-2016-2781"}, {"id": "DEBIAN-CVE-2025-5278", "summary": "heap overflow in sort"}]}]}]}`)
	}

	findings, err := DepsFindings("/scan", Run{}, report(unimportant))
	if err != nil || len(findings) != 0 {
		t.Fatalf("only unimportant advisories must pass as osv-scanner's exit 0 says: %v %v", findings, err)
	}

	findings, err = DepsFindings("/scan", Run{ExitCode: DepsVulnerableExit}, report(unimportant, `{"ids": ["DEBIAN-CVE-2025-5278"], "aliases": ["CVE-2025-5278"]}`))
	if err != nil {
		t.Fatal(err)
	}
	want := "coreutils@9.7-3 (Debian:13) has known vulnerabilities: DEBIAN-CVE-2025-5278 (CVE-2025-5278) heap overflow in sort"
	if len(findings) != 1 || findings[0].Message != want || findings[0].Location.File != "bom.cdx.json" {
		t.Fatalf("findings: %+v\nwant one: %s", findings, want)
	}
}

// Go modules are go-vuln's, and nothing about the scan may run the
// repository's code or reach beyond OSV's advisory lookup. Only a scan over
// the source itself excludes the skipped directories.
func TestDepsArgumentsLeaveGoToGoVuln(t *testing.T) {
	const exclusion = "--experimental-exclude=r:(^|/)(testdata|vendor|node_modules)(/|$)"
	for _, tree := range []DepsTree{DepsSource, DepsStaged} {
		args := DepsArguments("osv-scanner", false, "/tmp/report.json", tree)
		for _, want := range []string{"scan", "source", "--recursive", "--no-ignore", "--no-resolve", "--no-call-analysis=all", "--experimental-disable-plugins=go/gomod", "--experimental-disable-plugins=directory", "--format=json", "--output-file=/tmp/report.json"} {
			if !slices.Contains(args, want) {
				t.Errorf("%s: arguments %q lack %q", tree, args, want)
			}
		}
		if slices.Contains(args, exclusion) != (tree == DepsSource) {
			t.Errorf("%s: arguments %q exclude the skipped directories only when the scan covers the source itself", tree, args)
		}
		if args[0] != "osv-scanner" || args[len(args)-1] != "." || slices.ContainsFunc(args, func(arg string) bool { return strings.HasPrefix(arg, "--config") }) {
			t.Fatalf("%s: an unconfigured scan covers the working directory with osv-scanner's own lookup: %q", tree, args)
		}
		if args := DepsArguments("osv-scanner", true, "/tmp/report.json", tree); !slices.Contains(args, "--config=osv-scanner.toml") || args[len(args)-1] != "." {
			t.Fatalf("%s: a root osv-scanner.toml must be named: %q", tree, args)
		}
	}
}
