//go:build integration

package verify

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The native executor reaches the same verdicts as the Dagger self-test
// (runner/depsvuln.go) on the same fixtures. These need github.com for the
// pinned osv-scanner release and api.osv.dev for its advisories.
func TestNativeDepsVulnAgreesWithTheFixtures(t *testing.T) {
	shared := repositoryRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	native := &Native{Cache: &Cache{Dir: t.TempDir()}}

	t.Run("deps-vuln passes deps-clean despite its vulnerable go.mod", func(t *testing.T) {
		result := native.Execute(ctx, sourceRequest(t, shared, copyFixture(t, shared, "deps-clean"), CheckDepsVuln))
		if result.Status != StatusPassed {
			t.Fatalf("deps-clean fixture must pass deps-vuln: %+v", result)
		}
	})

	t.Run("deps-vuln fails deps-vulnerable once per package", func(t *testing.T) {
		result := native.Execute(ctx, sourceRequest(t, shared, copyFixture(t, shared, "deps-vulnerable"), CheckDepsVuln))
		if result.Status != StatusFailed {
			t.Fatalf("deps-vulnerable fixture must fail for its advisories, not a tool error: %+v", result)
		}
		findings := fixtureFindings(t, result)
		if len(findings) != 2 || findings[0].Location.File != "package-lock.json" || !strings.Contains(findings[0].Message, "lodash@4.17.20 (npm)") || !strings.Contains(findings[0].Message, "GHSA-35jh-r3h4-6jhm") || !strings.Contains(findings[1].Message, "minimist@1.2.5 (npm)") || strings.Contains(result.Stdout+string(result.Details), "golang.org/x/text") {
			t.Fatalf("deps-vulnerable must report lodash and minimist and nothing from go.mod: %+v", findings)
		}
	})

	t.Run("deps-vuln skips lockfiles under testdata", func(t *testing.T) {
		source := copyFixture(t, shared, "deps-clean")
		vulnerable := copyFixture(t, shared, "deps-vulnerable")
		lockfile, err := os.ReadFile(filepath.Join(vulnerable, "package-lock.json"))
		if err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(source, "web", "testdata", "package-lock.json"), string(lockfile))
		if result := native.Execute(ctx, sourceRequest(t, shared, source, CheckDepsVuln)); result.Status != StatusPassed {
			t.Fatalf("a fixture lockfile under testdata must not be scanned: %+v", result)
		}
	})

	t.Run("deps-vuln honors a root osv-scanner.toml", func(t *testing.T) {
		source := copyFixture(t, shared, "deps-vulnerable")
		writeTestFile(t, filepath.Join(source, "osv-scanner.toml"), "[[IgnoredVulns]]\nid = \"GHSA-xvch-5gv4-984h\"\nreason = \"The fixture never parses untrusted arguments.\"\n")
		result := native.Execute(ctx, sourceRequest(t, shared, source, CheckDepsVuln))
		findings := fixtureFindings(t, result)
		if result.Status != StatusFailed || len(findings) != 1 || !strings.Contains(findings[0].Message, "lodash@4.17.20") {
			t.Fatalf("an ignored advisory must drop minimist and keep lodash: %+v", result)
		}
	})

	t.Run("deps-vuln refuses a target without a lockfile", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "good", CheckDepsVuln))
		if result.Status != StatusError || !strings.Contains(result.Error, "found no supported dependency lockfile") {
			t.Fatalf("nothing to scan must error rather than pass: %+v", result)
		}
	})

	t.Run("deps-vuln is an error when OSV cannot be reached", func(t *testing.T) {
		req := sourceRequest(t, shared, copyFixture(t, shared, "deps-vulnerable"), CheckDepsVuln)
		req.Environment.PassEnv = nil
		req.Environment.Env = map[string]string{"HTTPS_PROXY": "http://127.0.0.1:1", "https_proxy": "http://127.0.0.1:1"}
		if result := native.Execute(ctx, req); result.Status != StatusError {
			t.Fatalf("a failed advisory query must be an error, never a pass or a finding: %+v", result)
		}
	})
}
