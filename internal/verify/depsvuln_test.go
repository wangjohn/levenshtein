package verify

import (
	"context"
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
	findings, err := depsFindings("/scan", depsVulnerableExit, []byte(depsSampleReport), "")
	if err != nil {
		t.Fatal(err)
	}

	want := []finding{
		{
			Code:     string(CheckDepsVuln),
			Message:  "lodash@4.17.20 (npm) has known vulnerabilities: GHSA-29mw-wpgm-hmr9 (CVE-2020-28500, severity 5.3) Regular Expression Denial of Service (ReDoS) in lodash; GHSA-35jh-r3h4-6jhm, GHSA-r5fr-rjxr-66jc (CVE-2021-23337, severity 8.1) Command Injection in lodash",
			Location: location{File: "web/package-lock.json", Line: 1},
		},
		{
			Code:     string(CheckDepsVuln),
			Message:  "minimist@1.2.5 (npm) has known vulnerabilities: GHSA-xvch-5gv4-984h Prototype Pollution in minimist",
			Location: location{File: "web/package-lock.json", Line: 1},
		},
	}
	if !slices.Equal(findings, want) {
		t.Fatalf("findings:\n%+v\nwant:\n%+v", findings, want)
	}
}

// A scan with nothing to scan, a failed advisory query, and a report that
// contradicts the exit code are all errors, never a pass.
func TestDepsFindingsSeparateFindingsFromToolErrors(t *testing.T) {
	if findings, err := depsFindings("/scan", 0, []byte(`{"results":[]}`), ""); err != nil || len(findings) != 0 {
		t.Fatalf("a clean scan is not a finding: %v %v", findings, err)
	}
	if _, err := depsFindings("/scan", depsNoPackagesExit, nil, "No package sources found"); err == nil || !strings.Contains(err.Error(), "found no supported dependency lockfile") {
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
		{depsVulnerableExit, `{"results":[]}`},
		{depsVulnerableExit, ""},
		{0, "not json"},
	} {
		if findings, err := depsFindings("/scan", tc.code, []byte(tc.report), "query failed"); err == nil {
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

	findings, err := depsFindings("/scan", 0, report(unimportant), "")
	if err != nil || len(findings) != 0 {
		t.Fatalf("only unimportant advisories must pass as osv-scanner's exit 0 says: %v %v", findings, err)
	}

	findings, err = depsFindings("/scan", depsVulnerableExit, report(unimportant, `{"ids": ["DEBIAN-CVE-2025-5278"], "aliases": ["CVE-2025-5278"]}`), "")
	if err != nil {
		t.Fatal(err)
	}
	want := "coreutils@9.7-3 (Debian:13) has known vulnerabilities: DEBIAN-CVE-2025-5278 (CVE-2025-5278) heap overflow in sort"
	if len(findings) != 1 || findings[0].Message != want || findings[0].Location.File != "bom.cdx.json" {
		t.Fatalf("findings: %+v\nwant one: %s", findings, want)
	}
}

// Go modules are go-vuln's, and nothing about the scan may run the
// repository's code or reach beyond OSV's advisory lookup.
func TestDepsArgumentsLeaveGoToGoVuln(t *testing.T) {
	args := depsArguments("osv-scanner", false, "/tmp/report.json")
	for _, want := range []string{"scan", "source", "--recursive", "--no-ignore", "--experimental-exclude=r:(^|/)(testdata|vendor|node_modules)(/|$)", "--no-resolve", "--no-call-analysis=all", "--experimental-disable-plugins=go/gomod", "--experimental-disable-plugins=directory", "--format=json", "--output-file=/tmp/report.json"} {
		if !slices.Contains(args, want) {
			t.Errorf("arguments %q lack %q", args, want)
		}
	}
	if args[len(args)-1] != "." || slices.ContainsFunc(args, func(arg string) bool { return strings.HasPrefix(arg, "--config") }) {
		t.Fatalf("an unconfigured scan covers the working directory with osv-scanner's own lookup: %q", args)
	}
	if args := depsArguments("osv-scanner", true, "/tmp/report.json"); !slices.Contains(args, "--config=osv-scanner.toml") {
		t.Fatalf("a root osv-scanner.toml must be named: %q", args)
	}
}

func TestDepsEnvDropsHostSettings(t *testing.T) {
	env := depsEnv([]string{"PATH=/bin", "OSV_SCANNER_LOCAL_DB_CACHE_DIRECTORY=/elsewhere", "HTTPS_PROXY=http://proxy"})
	if !slices.Equal(env, []string{"PATH=/bin", "HTTPS_PROXY=http://proxy"}) {
		t.Fatalf("unexpected osv-scanner environment: %v", env)
	}
}

// deps-vuln queries current advisory data, which no input fingerprint covers,
// so like go-vuln its verdict is never reused on either executor and its
// Dagger call is never answered from Dagger's own cache.
func TestDependencyScansNeverReuseAVerdict(t *testing.T) {
	for _, kind := range []ExecutorKind{ExecutorDagger, ExecutorNative} {
		req := cacheRequest(t)
		req.Check.Kind = CheckDepsVuln
		req.Environment.Executor = kind
		executor := &countingExecutor{status: StatusPassed}
		runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}

		for range 2 {
			result := runner.Execute(context.Background(), req)
			if result.Status != StatusPassed || result.Cache.Status != CacheDisabled || result.Cache.Reason == "" {
				t.Fatalf("%s: unexpected deps-vuln result: %+v", kind, result)
			}
		}
		if executor.calls != 2 {
			t.Fatalf("%s: reused a deps-vuln verdict: %d calls", kind, executor.calls)
		}
	}

	req := cacheRequest(t)
	req.Check.Kind = CheckDepsVuln
	if first, second := executionNonce(req), executionNonce(req); first == "" || first == second {
		t.Fatal("deps-vuln must get unique Dagger execution inputs")
	}
}
