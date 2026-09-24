package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// depsConfig is the osv-scanner configuration deps-vuln honors at the root.
const depsConfig = "osv-scanner.toml"

// osv-scanner's documented exit codes: 1 for vulnerabilities found, 128 for
// no packages found. Everything else nonzero is an error.
const (
	depsVulnerableExit = 1
	depsNoPackagesExit = 128
)

func (n *Native) depsVuln(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	files, err := visibleFiles(req.Source, req.Target.Inputs, req.Target.Exclude, sourceSkipDir)
	if err != nil {
		return nil, toolRun{}, err
	}
	binary, err := installRelease(ctx, req.Shared, work.Root, releaseOSVScanner, "")
	if err != nil {
		return nil, toolRun{}, err
	}

	// osv-scanner walks a directory and finds its own configuration there, so
	// it runs over a copy holding only what the Dagger path would import.
	staged, cleanup, err := stageFiles(req.Source, files)
	if err != nil {
		return nil, toolRun{}, err
	}
	defer cleanup()
	staged, err = filepath.EvalSymlinks(staged)
	if err != nil {
		return nil, toolRun{}, err
	}
	reports, err := os.MkdirTemp("", "levenshtein-deps-")
	if err != nil {
		return nil, toolRun{}, err
	}
	defer func() { _ = os.RemoveAll(reports) }()
	report := filepath.Join(reports, "report.json")

	args := depsArguments(binary, slices.Contains(files, depsConfig), report)
	run, err := runTool(ctx, staged, args, depsEnv(work.Env), goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	data, err := os.ReadFile(report)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, run, err
	}
	findings, err := depsFindings(staged, run.ExitCode, data, run.Stderr)
	return findings, run, err
}

// depsEnv drops osv-scanner's own OSV_SCANNER_* settings, which the container
// does not have either.
func depsEnv(env []string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(entry string) bool {
		return strings.HasPrefix(entry, "OSV_SCANNER_")
	})
}

// depsArguments scans every lockfile under the working directory, including
// ones .gitignore names, since the target's inputs and excludes already decide
// what is in scope, except under the sourceSkipDirs, which the native path
// does not stage either. Go modules are left to go-vuln, whose govulncheck reports
// only vulnerabilities the code can reach: the go/gomod extractor is disabled,
// and call analysis, which would run govulncheck or a Rust build, is off for
// every language. The directory preset is disabled too: it would identify
// vendored C/C++ code by hashing it through the OSV API, and walk git history.
// --no-resolve keeps a manifest to the versions it names rather than resolving
// transitive dependencies through deps.dev. A root osv-scanner.toml is named
// explicitly and applies to every lockfile; without one, osv-scanner reads the
// osv-scanner.toml beside each lockfile, if there is one.
// runner/depsvuln.go keeps a copy; change both together.
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
	IDs         []string                `json:"ids"`
	Aliases     []string                `json:"aliases"`
	MaxSeverity string                  `json:"max_severity"`
	Analysis    map[string]depsAnalysis `json:"experimental_analysis"`
}

// depsAnalysis is what osv-scanner concluded about one advisory in a group:
// whether the code calls the vulnerable function, and whether the
// distribution rates the advisory unimportant, as Debian does.
type depsAnalysis struct {
	Called      bool `json:"called"`
	Unimportant bool `json:"unimportant"`
}

// counts reports whether osv-scanner counts a group toward its exit code: a
// group is left out when no advisory in it is called or any is unimportant.
// osv-scanner still lists such a group, so deps-vuln leaves it out too, or it
// would report what osv-scanner judged needs no action and contradict its
// exit code.
func (g depsGroup) counts() bool {
	if len(g.Analysis) == 0 {
		return true
	}
	called := false
	for _, analysis := range g.Analysis {
		if analysis.Unimportant {
			return false
		}
		called = called || analysis.Called
	}
	return called
}

type depsAdvisory struct {
	ID      string `json:"id"`
	Summary string `json:"summary"`
}

// depsFindings turns one osv-scanner run into findings, one per vulnerable
// package version, located at the lockfile that pins it and naming every
// advisory osv-scanner counts (see depsGroups). osv-scanner exits 0 with nothing to report and 1 with
// vulnerabilities. 128 means it found no supported lockfile, which is an
// error rather than an empty pass, as a Go check without packages is; so is
// any other exit, including a failed advisory query, a missing or unreadable
// report, and a report that contradicts the exit code. root is the scanned
// directory, which lockfile paths are reported relative to.
// runner/depsvuln.go keeps a copy; change both together.
func depsFindings(root string, exitCode int, report []byte, stderr string) ([]finding, error) {
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

	var findings []finding
	for _, result := range parsed.Results {
		file := strings.TrimPrefix(filepath.ToSlash(repositoryPath(root, result.Source.Path)), "./")
		for _, pkg := range result.Packages {
			groups := depsGroups(pkg)
			if len(groups) == 0 {
				continue
			}
			if file == "" || pkg.Package.Name == "" {
				return nil, fmt.Errorf("unexpected osv-scanner result for %q in %q", pkg.Package.Name, result.Source.Path)
			}
			findings = append(findings, finding{
				Code:     string(CheckDepsVuln),
				Message:  depsMessage(pkg, groups),
				Location: location{File: file, Line: 1},
			})
		}
	}
	if (exitCode == 0) != (len(findings) == 0) {
		return nil, fmt.Errorf("osv-scanner exit %d does not match its %d vulnerable packages: %s", exitCode, len(findings), strings.TrimSpace(stderr))
	}
	return findings, nil
}

// depsGroups are the groups of a package's advisories that osv-scanner counts
// toward its exit code, or, for a report without groups, one group per
// advisory. runner/depsvuln.go keeps a copy; change both together.
func depsGroups(pkg depsPackage) []depsGroup {
	if len(pkg.Vulnerabilities) == 0 {
		return nil
	}
	if len(pkg.Groups) == 0 {
		groups := make([]depsGroup, 0, len(pkg.Vulnerabilities))
		for _, advisory := range pkg.Vulnerabilities {
			groups = append(groups, depsGroup{IDs: []string{advisory.ID}})
		}
		return groups
	}
	return slices.DeleteFunc(slices.Clone(pkg.Groups), func(group depsGroup) bool { return !group.counts() })
}

// depsMessage names the package version and, per vulnerability, its advisory
// IDs, the other IDs it is known by, its highest severity score, and its
// summary. The IDs are what an osv-scanner.toml [[IgnoredVulns]] entry takes.
func depsMessage(pkg depsPackage, groups []depsGroup) string {
	summaries := map[string]string{}
	for _, advisory := range pkg.Vulnerabilities {
		summaries[advisory.ID] = strings.TrimSpace(advisory.Summary)
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
