package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	"dagger/levenshtein/internal/dagger"
)

// depsConfig is the osv-scanner configuration deps-vuln honors at the root.
const depsConfig = "osv-scanner.toml"

// osv-scanner's documented exit codes: 1 for vulnerabilities found, 128 for
// no packages found. Everything else nonzero is an error.
const (
	depsVulnerableExit = 1
	depsNoPackagesExit = 128
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
	configs, err := source.Glob(ctx, depsConfig)
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

	checked := ctr.WithExec(depsArguments("/usr/local/bin/osv-scanner", len(configs) == 1, depsReportPath), dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
	exitCode, err := checked.ExitCode(ctx)
	if err != nil {
		return nil, err
	}
	stderr, err := checked.Stderr(ctx)
	if err != nil {
		return nil, err
	}
	var report []byte
	if exitCode == 0 || exitCode == depsVulnerableExit {
		contents, err := checked.File(depsReportPath).Contents(ctx)
		if err != nil {
			return nil, fmt.Errorf("osv-scanner exited %d without a report: %w: %s", exitCode, err, strings.TrimSpace(stderr))
		}
		report = []byte(contents)
	}
	return depsFindings(exitCode, report, stderr)
}

// depsArguments scans every lockfile under the working directory, including
// ones .gitignore names, since the target's inputs and excludes already decide
// what is in scope, except under the sourceSkipDirs. Go modules are left to go-vuln: the go/gomod extractor is
// disabled, and call analysis, which would run govulncheck or a Rust build, is
// off for every language. The directory preset, which hashes vendored C/C++
// code through the OSV API, is disabled too, and --no-resolve keeps a manifest
// to the versions it names. A root osv-scanner.toml is named explicitly and
// applies to every lockfile. internal/verify/depsvuln.go keeps a copy; change
// both together.
func depsArguments(binary string, configured bool, report string) []string {
	args := []string{binary, "scan", "source", "--recursive", "--no-ignore", "--experimental-exclude=r:(^|/)(" + strings.Join(sourceSkipDirs, "|") + ")(/|$)", "--no-resolve", "--no-call-analysis=all", "--experimental-disable-plugins=go/gomod", "--experimental-disable-plugins=directory", "--format=json", "--output-file=" + report, "--verbosity=warn"}
	if configured {
		args = append(args, "--config="+depsConfig)
	}
	return append(args, ".")
}

// depsReport is the part of osv-scanner's JSON output deps-vuln reads. The tags
// are osv-scanner's field names.
type depsReport struct {
	Results []depsResult `json:"results"`
}

type depsResult struct {
	Source   depsSource    `json:"source"`
	Packages []depsPackage `json:"packages"`
}

type depsSource struct {
	Path string `json:"path"`
}

type depsPackage struct {
	Package         depsPackageInfo `json:"package"`
	Groups          []depsGroup     `json:"groups"`
	Vulnerabilities []depsAdvisory  `json:"vulnerabilities"`
}

type depsPackageInfo struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Ecosystem string `json:"ecosystem"`
}

// depsGroup is one set of advisories osv-scanner considers the same
// vulnerability, such as a GHSA and the CVE it aliases.
type depsGroup struct {
	IDs         []string `json:"ids"`
	Aliases     []string `json:"aliases"`
	MaxSeverity string   `json:"max_severity"`
}

type depsAdvisory struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

// depsFindings turns one osv-scanner run into findings, one per vulnerable
// package version, located at the lockfile that pins it and naming every
// advisory. 128, no supported lockfile, is an error rather than an empty pass,
// and so is any other exit, a missing or unreadable report, and a report that
// contradicts the exit code. Lockfile paths are reported relative to depsRoot.
// internal/verify/depsvuln.go keeps a copy; change both together.
func depsFindings(exitCode int, report []byte, stderr string) ([]diagnostic, error) {
	switch exitCode {
	case 0, depsVulnerableExit:
	case depsNoPackagesExit:
		return nil, fmt.Errorf("deps-vuln found no supported dependency lockfile to scan; Go modules are go-vuln's: %s", strings.TrimSpace(stderr))
	default:
		return nil, fmt.Errorf("osv-scanner exited %d: %s", exitCode, strings.TrimSpace(stderr))
	}

	var parsed depsReport
	if err := json.Unmarshal(report, &parsed); err != nil {
		return nil, fmt.Errorf("osv-scanner exited %d without a readable report: %w: %s", exitCode, err, strings.TrimSpace(stderr))
	}

	var findings []diagnostic
	for _, result := range parsed.Results {
		file := result.Source.Path
		if rel, ok := strings.CutPrefix(path.Clean(file), depsRoot+"/"); ok {
			file = rel
		}
		file = strings.TrimPrefix(file, "./")
		for _, pkg := range result.Packages {
			if len(pkg.Vulnerabilities) == 0 {
				continue
			}
			if file == "" || pkg.Package.Name == "" {
				return nil, fmt.Errorf("unexpected osv-scanner result for %q in %q", pkg.Package.Name, result.Source.Path)
			}
			findings = append(findings, diagnostic{
				Code:     string(checkDepsVuln),
				Message:  depsMessage(pkg),
				Location: location{File: file, Line: 1},
			})
		}
	}
	if (exitCode == 0) != (len(findings) == 0) {
		return nil, fmt.Errorf("osv-scanner exit %d does not match its %d vulnerable packages: %s", exitCode, len(findings), strings.TrimSpace(stderr))
	}
	return findings, nil
}

// depsMessage names the package version and, per vulnerability, its advisory
// IDs, the other IDs it is known by, its highest severity score, and its
// summary. internal/verify/depsvuln.go keeps a copy; change both together.
func depsMessage(pkg depsPackage) string {
	summaries := map[string]string{}
	for _, advisory := range pkg.Vulnerabilities {
		summaries[advisory.ID] = strings.TrimSpace(advisory.Summary)
	}
	groups := pkg.Groups
	if len(groups) == 0 {
		for _, advisory := range pkg.Vulnerabilities {
			groups = append(groups, depsGroup{IDs: []string{advisory.ID}})
		}
	}

	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		var details []string
		for _, alias := range group.Aliases {
			if !slices.Contains(group.IDs, alias) {
				details = append(details, alias)
			}
		}
		if group.MaxSeverity != "" {
			details = append(details, "severity "+group.MaxSeverity)
		}

		part := strings.Join(group.IDs, ", ")
		if len(details) > 0 {
			part = fmt.Sprintf("%s (%s)", part, strings.Join(details, ", "))
		}
		if i := slices.IndexFunc(group.IDs, func(id string) bool { return summaries[id] != "" }); i >= 0 {
			part = fmt.Sprintf("%s %s", part, summaries[group.IDs[i]])
		}
		parts = append(parts, part)
	}
	return fmt.Sprintf("%s@%s (%s) has known vulnerabilities: %s", pkg.Package.Name, pkg.Package.Version, pkg.Package.Ecosystem, strings.Join(parts, "; "))
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
