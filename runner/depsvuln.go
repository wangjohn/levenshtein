package main

import (
	"context"
	"fmt"
	"strings"

	"dagger/levenshtein/internal/checktool"
	"dagger/levenshtein/internal/dagger"
)

// depsReportPath is where osv-scanner writes its report, outside the scanned
// source at depsRoot.
const (
	depsReportPath = "/tmp/levenshtein-deps.json"
	depsRoot       = "/src"
)

// depsVuln scans the source's non-Go dependency lockfiles for known
// vulnerabilities with the pinned osv-scanner. It queries current advisory
// data, so SharedCheck requires a nonce and Dagger never answers it from its
// cache.
func depsVuln(ctx context.Context, source *dagger.Directory, tools toolchain, nonce string) ([]diagnostic, error) {
	configs, err := source.Glob(ctx, checktool.DepsConfig)
	if err != nil {
		return nil, err
	}

	ctr, err := withRelease(ctx, dag.Container().From(tools.GoImage), tools.OSVScanner, "osv-scanner", "/usr/local/bin/osv-scanner")
	if err != nil {
		return nil, err
	}
	ctr = ctr.WithDirectory(depsRoot, source).
		WithWorkdir(depsRoot).
		WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)

	args := checktool.DepsArguments("/usr/local/bin/osv-scanner", len(configs) == 1, depsReportPath, checktool.DepsSource)
	run, checked, err := runTool(ctx, ctr, args)
	if err != nil {
		return nil, err
	}
	var report []byte
	if run.ExitCode == 0 || run.ExitCode == checktool.DepsVulnerableExit {
		contents, err := checked.File(depsReportPath).Contents(ctx)
		if err != nil {
			return nil, fmt.Errorf("osv-scanner exited %d without a report: %w: %s", run.ExitCode, err, strings.TrimSpace(run.Stderr))
		}
		report = []byte(contents)
	}
	return checktool.DepsFindings(depsRoot, run, report)
}

// depsFixture is one of the dependency fixtures with its files under their
// real names. They are stored with a .fixture suffix so that GitHub's
// dependency graph never reads them as Levenshtein's own dependencies.
func depsFixture(ctx context.Context, fixtures *dagger.Directory, name string) (*dagger.Directory, error) {
	dir := fixtures.Directory(name)
	files, err := dir.Glob(ctx, "*.fixture")
	if err != nil {
		return nil, err
	}
	out := dag.Directory()
	for _, file := range files {
		out = out.WithFile(strings.TrimSuffix(file, ".fixture"), dir.File(file))
	}
	return out, nil
}

// depsVulnSelfTest proves the pinned osv-scanner downloads, verifies and
// queries OSV: deps-clean passes even though its go.mod requires a vulnerable
// module, which is go-vuln's to report, and deps-vulnerable reports each of its
// two vulnerable npm packages once, at the lockfile, with its advisories.
func depsVulnSelfTest(ctx context.Context, fixtures *dagger.Directory, tools toolchain, nonce string) error {
	clean, err := depsFixture(ctx, fixtures, "deps-clean")
	if err != nil {
		return err
	}
	passed, err := depsVuln(ctx, clean, tools, nonce)
	if err != nil || len(passed) != 0 {
		return fmt.Errorf("deps-clean fixture must pass deps-vuln: findings=%v error=%v", passed, err)
	}

	vulnerable, err := depsFixture(ctx, fixtures, "deps-vulnerable")
	if err != nil {
		return err
	}
	failed, err := depsVuln(ctx, vulnerable, tools, nonce)
	if err != nil {
		return fmt.Errorf("deps-vulnerable fixture must fail for its advisories, not a tool error: %w", err)
	}
	if len(failed) != 2 || failed[0].Location.File != "package-lock.json" || !strings.Contains(failed[0].Message, "lodash@4.17.20 (npm)") || !strings.Contains(failed[0].Message, "GHSA-35jh-r3h4-6jhm") || !strings.Contains(failed[1].Message, "minimist@1.2.5 (npm)") || !strings.Contains(failed[1].Message, "GHSA-xvch-5gv4-984h") {
		return fmt.Errorf("deps-vulnerable fixture must report lodash and minimist at package-lock.json and nothing from go.mod: %v", failed)
	}
	return nil
}
