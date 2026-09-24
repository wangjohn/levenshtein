package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"

	"dagger/levenshtein/internal/dagger"
)

// Community rules run in levenshtein-community-lint, a second linter built
// from runner/community and the rule modules a configuration pins. It runs in
// its own process on the same pinned Staticcheck, and its findings merge into
// the core linter's. See docs/community-rules.md.

// ruleModule is one rule module a go-lint check runs, in the JSON the CLI
// sends (internal/verify's PlannedRuleModule) and the community linter reads
// (runner/community's ModuleConfig); change all three together.
type ruleModule struct {
	Path      string                       `json:"path"`
	Version   string                       `json:"version"`
	Namespace string                       `json:"namespace"`
	Select    []string                     `json:"select"`
	Advisory  []string                     `json:"advisory,omitempty"`
	Settings  map[string]map[string]string `json:"settings,omitempty"`
}

// buildModule is one entry of the community build's request
// (runner/community/internal/build's Module). Dir is only ever set by the
// self-test, for a fixture module that has no published version.
type buildModule struct {
	Path      string `json:"path"`
	Version   string `json:"version"`
	Namespace string `json:"namespace"`
	Dir       string `json:"dir,omitempty"`
}

// communityPattern is one community selection pattern. runner/community has
// the original (Pattern); runner/testdata/community-patterns.json keeps the
// copies in step.
var communityPattern = regexp.MustCompile(`^-?[A-Za-z]+_([A-Za-z0-9_]*\*|[A-Za-z][A-Za-z0-9_]*)$`)

// parseRuleModules reads the ruleModules argument. Unknown fields are refused,
// so a caller cannot reach the build's local-directory replacement.
func parseRuleModules(raw string) ([]ruleModule, error) {
	if raw == "" {
		return nil, nil
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var modules []ruleModule
	if err := decoder.Decode(&modules); err != nil {
		return nil, fmt.Errorf("invalid ruleModules argument: %w", err)
	}
	for _, module := range modules {
		if module.Path == "" || module.Version == "" || module.Namespace == "" || len(module.Select) == 0 {
			return nil, fmt.Errorf("rule module %q needs a version, a namespace, and a select list", module.Path)
		}
	}
	return modules, nil
}

// splitChecks divides a go-lint check's patterns between the linters: a
// pattern with "_" names community rules, and every core code is free of it.
func splitChecks(checks []string, modules []ruleModule) (core, community []string, err error) {
	for _, check := range checks {
		if !strings.Contains(check, "_") {
			core = append(core, check)
			continue
		}
		if !communityPattern.MatchString(check) {
			return nil, nil, fmt.Errorf("go-lint check %q must be one community pattern such as \"errs_*\", \"errs_no*\", or \"-errs_nopanic\"", check)
		}
		namespace, _, _ := strings.Cut(strings.ToLower(strings.TrimPrefix(check, "-")), "_")
		if !slices.ContainsFunc(modules, func(module ruleModule) bool { return module.Namespace == namespace }) {
			return nil, nil, fmt.Errorf("go-lint check %q names namespace %q, which no rule module declares", check, namespace)
		}
		community = append(community, check)
	}
	return core, community, nil
}

// warningKind names a problem a check result reports without failing;
// internal/verify's WarningKind lists the same kinds.
type warningKind string

const (
	warningRuleModuleDeprecated warningKind = "rule-module-deprecated"
	warningRuleModuleRetracted  warningKind = "rule-module-retracted"
)

type warning struct {
	Kind    warningKind `json:"kind"`
	Message string      `json:"message"`
}

//go:embed rule-modules.json
var ruleModulesJSON []byte

// ruleModuleNotice is one entry of rule-modules.json. The CLI refuses a
// withdrawn version before anything runs; a deprecated one runs and warns.
type ruleModuleNotice struct {
	Path        string       `json:"path"`
	Version     string       `json:"version"`
	Status      noticeStatus `json:"status"`
	Reason      string       `json:"reason"`
	Replacement string       `json:"replacement,omitempty"`
}

// noticeStatus is what a release says about one rule module version;
// internal/verify's RuleModuleStatus lists the same statuses.
type noticeStatus string

const (
	noticeDeprecated noticeStatus = "deprecated"
	noticeWithdrawn  noticeStatus = "withdrawn"
)

// releaseNotices applies rule-modules.json to a check's pins: a deprecated
// version warns, and a withdrawn one is refused here too, so a direct Dagger
// call cannot run what the CLI would refuse.
func releaseNotices(modules []ruleModule) ([]warning, error) {
	var shipped struct {
		Modules []ruleModuleNotice `json:"modules"`
	}
	if err := json.Unmarshal(ruleModulesJSON, &shipped); err != nil {
		return nil, fmt.Errorf("rule-modules.json: %w", err)
	}

	var warnings []warning
	for _, module := range modules {
		for _, notice := range shipped.Modules {
			if notice.Path != module.Path || notice.Version != module.Version {
				continue
			}
			if notice.Status == noticeWithdrawn {
				return nil, fmt.Errorf("%s@%s is withdrawn: %s", module.Path, module.Version, notice.Reason)
			}
			message := fmt.Sprintf("%s@%s is deprecated: %s", module.Path, module.Version, notice.Reason)
			if notice.Replacement != "" {
				message += "; use " + notice.Replacement + " instead"
			}
			warnings = append(warnings, warning{Kind: warningRuleModuleDeprecated, Message: message})
		}
	}
	return warnings, nil
}

// communityBuild is the linter built for a set of modules, with what the
// build learned about them.
type communityBuild struct {
	Binary   *dagger.File
	Warnings []warning
}

// buildResult is runner/community/internal/build's Result.
type buildResult struct {
	Retracted []struct {
		Module    string   `json:"module"`
		Version   string   `json:"version"`
		Rationale []string `json:"rationale"`
	} `json:"retracted"`
}

// execFailure turns a failed command into its own error output, so a message
// names what failed rather than the whole Dagger call. The build and download
// steps let their commands fail instead of expecting any exit code: Dagger
// never caches a failed command, so a transient failure, such as the module
// proxy being unreachable, is retried on the next run instead of being
// replayed until the pins change.
func execFailure(err error) error {
	var failed *dagger.ExecError
	if errors.As(err, &failed) {
		if stderr := strings.TrimSpace(failed.Stderr); stderr != "" {
			return errors.New(stderr)
		}
	}
	return err
}

// withNonce makes a fresh run re-execute a step Dagger would otherwise answer
// from its cache: the build asks the proxy about retractions, and the download
// asks it for the current state of every dependency.
func withNonce(ctr *dagger.Container, nonce string) *dagger.Container {
	if nonce == "" {
		return ctr
	}
	return ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
}

// buildCommunityLinter compiles levenshtein-community-lint in a container with
// network access and the shared Go caches. Nothing here runs a rule module's
// code: go mod and go build only read and compile it. The build never sees the
// consumer's source, so every check with the same pins shares one build.
func buildCommunityLinter(ctx context.Context, tools toolchain, modules []buildModule, fixtures *dagger.Directory, nonce string) (communityBuild, error) {
	request, err := json.Marshal(struct {
		Go          string        `json:"go"`
		Staticcheck string        `json:"staticcheck"`
		Community   string        `json:"community"`
		Modules     []buildModule `json:"modules"`
	}{tools.Go, tools.Staticcheck, "/community", modules})
	if err != nil {
		return communityBuild{}, err
	}

	ctr := goContainer(tools).
		WithDirectory("/community", dag.CurrentModule().Source().Directory("community")).
		WithWorkdir("/community").
		WithExec([]string{"go", "build", "-trimpath", "-o", "/usr/local/bin/levenshtein-community-build", "./cmd/levenshtein-community-build"}).
		WithNewFile("/build/request.json", string(request))
	if fixtures != nil {
		ctr = ctr.WithDirectory("/fixtures", fixtures)
	}
	built := withNonce(ctr, nonce).WithExec([]string{"levenshtein-community-build", "-request", "/build/request.json", "-out", "/build/out"})
	summary, err := built.File("/build/out/build.json").Contents(ctx)
	if err != nil {
		return communityBuild{}, execFailure(err)
	}
	var result buildResult
	if err := json.Unmarshal([]byte(summary), &result); err != nil {
		return communityBuild{}, fmt.Errorf("reading the community build's summary: %w", err)
	}
	var warnings []warning
	for _, retracted := range result.Retracted {
		warnings = append(warnings, warning{
			Kind:    warningRuleModuleRetracted,
			Message: fmt.Sprintf("the author of %s retracted %s: %s", retracted.Module, retracted.Version, strings.Join(retracted.Rationale, "; ")),
		})
	}
	return communityBuild{Binary: built.File("/build/out/levenshtein-community-lint"), Warnings: warnings}, nil
}

// manifests are the files Go needs to resolve a module's dependencies.
var manifests = []string{"**/go.mod", "**/go.sum", "**/go.work", "**/go.work.sum"}

// downloadDependencies fetches the consumer module's dependencies, checked
// against its go.sum, into a plain directory the lint step can copy. It sees
// only the manifests, so an edit to any other file reuses the download.
//
// A vendored module needs no download: Go reads vendor/ in the lint step, and
// its dependencies may well be private modules no proxy can serve.
func downloadDependencies(ctx context.Context, source *dagger.Directory, module string, tools toolchain, nonce string) (*dagger.Directory, error) {
	exists := func(file string) (bool, error) {
		return source.Exists(ctx, file, dagger.DirectoryExistsOpts{ExpectedType: dagger.ExistsTypeRegularType})
	}
	modules, err := vendorFile(module, exists)
	if err != nil {
		return nil, err
	}
	vendored, err := exists(modules)
	if err != nil {
		return nil, err
	}
	if vendored {
		return dag.Directory(), nil
	}

	ctr := dag.Container().From(tools.GoImage).
		WithEnvVariable("GOTOOLCHAIN", "local").
		WithEnvVariable("GOMODCACHE", "/deps").
		WithExec([]string{"mkdir", "-p", "/deps"}).
		WithDirectory("/src", source.Filter(dagger.DirectoryFilterOpts{Include: manifests})).
		WithWorkdir(path.Join("/src", module))
	downloaded, err := withNonce(ctr, nonce).WithExec([]string{"go", "mod", "download"}).Sync(ctx)
	if err != nil {
		return nil, fmt.Errorf("downloading module %q's dependencies for the community linter failed: %w", module, execFailure(err))
	}
	return downloaded.Directory("/deps"), nil
}

// vendorFile is the vendor/modules.txt Go reads for module. In workspace mode
// Go reads only the workspace root's vendor directory, so the nearest go.work
// at or above the module decides; without one, it is the module's own. The
// lint step sets no GOWORK, so Go finds the same go.work.
func vendorFile(module string, exists func(string) (bool, error)) (string, error) {
	for dir := module; ; dir = path.Dir(dir) {
		workspace, err := exists(path.Join(dir, "go.work"))
		if err != nil {
			return "", err
		}
		if workspace {
			return path.Join(dir, "vendor", "modules.txt"), nil
		}
		if dir == "." {
			return path.Join(module, "vendor", "modules.txt"), nil
		}
	}
}

// communityConfig is runner/community's Config.
type communityConfig struct {
	Modules []ruleModule `json:"modules"`
	Checks  []string     `json:"checks,omitempty"`
}

// communityRun is one community lint step's output.
type communityRun struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Report   string
	Reported bool
}

// runCommunityLinter lints the consumer's source in the pinned Go image with
// nothing shared mounted: no module cache, build cache, or core Staticcheck
// cache, and no credentials. The dependencies arrive as a plain directory and
// GOPROXY is off, so the step never fetches anything. Its Staticcheck cache is
// a volume of its own, keyed by the linter binary, so only code that would run
// in this step anyway can ever write to it; it is private to one step at a
// time.
func runCommunityLinter(ctx context.Context, source *dagger.Directory, module string, tools toolchain, binary *dagger.File, deps *dagger.Directory, cfg communityConfig, nonce string) (communityRun, error) {
	config, err := json.Marshal(cfg)
	if err != nil {
		return communityRun{}, err
	}

	ctr := dag.Container().From(tools.GoImage).
		WithEnvVariable("GOTOOLCHAIN", "local").
		WithEnvVariable("GOPROXY", "off").
		WithEnvVariable("GOSUMDB", "off").
		WithEnvVariable("GOFLAGS", "").
		WithEnvVariable("GOMODCACHE", "/deps").
		WithEnvVariable("GOCACHE", "/tmp/go-build").
		WithDirectory("/deps", deps).
		WithFile("/usr/local/bin/levenshtein-community-lint", binary).
		WithNewFile("/lvrules/config.json", string(config)).
		WithDirectory("/src", source).
		WithWorkdir(path.Join("/src", module))
	if nonce == "" {
		digest, err := binary.Digest(ctx)
		if err != nil {
			return communityRun{}, err
		}
		volume := dag.CacheVolume("levenshtein-community-staticcheck-" + strings.ReplaceAll(digest, ":", "-"))
		ctr = ctr.WithMountedCache("/staticcheck", volume, dagger.ContainerWithMountedCacheOpts{Sharing: dagger.CacheSharingModePrivate}).
			WithEnvVariable("STATICCHECK_CACHE", "/staticcheck")
	} else {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce).
			WithEnvVariable("STATICCHECK_CACHE", "/tmp/staticcheck-fresh")
	}

	checked := ctr.WithExec([]string{"levenshtein-community-lint", "-f=json", "-lvrules.config=/lvrules/config.json", "-lvrules.report=/lvrules/report.json", "./..."}, dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
	exitCode, err := checked.ExitCode(ctx)
	if err != nil {
		return communityRun{}, err
	}
	stdout, err := checked.Stdout(ctx)
	if err != nil {
		return communityRun{}, err
	}
	stderr, err := checked.Stderr(ctx)
	if err != nil {
		return communityRun{}, err
	}
	reported, err := checked.Directory("/lvrules").Exists(ctx, "report.json", dagger.DirectoryExistsOpts{ExpectedType: dagger.ExistsTypeRegularType})
	if err != nil {
		return communityRun{}, err
	}
	report := ""
	if reported {
		if report, err = checked.File("/lvrules/report.json").Contents(ctx); err != nil {
			return communityRun{}, err
		}
	}
	return communityRun{ExitCode: exitCode, Stdout: stdout, Stderr: stderr, Report: report, Reported: reported}, nil
}

// communityReport is runner/community's Report.
type communityReport struct {
	Rules []struct {
		Code     string `json:"code"`
		Source   string `json:"source"`
		URL      string `json:"url"`
		Advisory bool   `json:"advisory"`
	} `json:"rules"`
	Warnings []warning `json:"warnings"`
	Failures []struct {
		Code    string `json:"code"`
		Source  string `json:"source"`
		Package string `json:"package"`
		Error   string `json:"error"`
	} `json:"failures"`
}

// communityDiagnostic is one line of the community linter's JSON output.
type communityDiagnostic struct {
	Code     string   `json:"code"`
	Severity string   `json:"severity"`
	Message  string   `json:"message"`
	Location location `json:"location"`
}

// Codes and severities in Staticcheck's JSON output.
const (
	staticcheckCode = "staticcheck"
	compileCode     = "compile"
	severityWarning = "warning"
)

// staleDirective is how Staticcheck reports an ignore directive that
// suppressed nothing.
const staleDirective = "this linter directive didn't match anything; should it be removed?"

// parseCommunity turns one community lint step into findings and warnings,
// refusing any result that does not agree with itself, as parseFindings does
// for the core linter. A run without a report did not finish, whatever its
// exit code: a rule may have called os.Exit. A rule that failed on any package
// makes the check an error.
func parseCommunity(run communityRun) ([]diagnostic, []warning, error) {
	if !run.Reported {
		return nil, nil, fmt.Errorf("the community linter exited %d without finishing (no completion report): %s", run.ExitCode, strings.TrimSpace(run.Stderr))
	}
	var report communityReport
	if err := json.Unmarshal([]byte(run.Report), &report); err != nil {
		return nil, nil, fmt.Errorf("the community linter's report is not valid JSON: %w", err)
	}
	if len(report.Failures) > 0 {
		var failed []string
		for _, failure := range report.Failures {
			failed = append(failed, fmt.Sprintf("%s (%s) failed on %s: %s", failure.Code, failure.Source, failure.Package, failure.Error))
		}
		return nil, report.Warnings, errors.New(strings.Join(failed, "; "))
	}
	if run.ExitCode != 0 && run.ExitCode != 1 {
		return nil, report.Warnings, fmt.Errorf("the community linter exited %d: %s\n%s", run.ExitCode, strings.TrimSpace(run.Stderr), run.Stdout)
	}
	if strings.TrimSpace(run.Stderr) != "" {
		return nil, report.Warnings, fmt.Errorf("the community linter could not produce a clean result: %s", run.Stderr)
	}

	rules := map[string]int{}
	for i, rule := range report.Rules {
		rules[strings.ToLower(rule.Code)] = i
	}
	var findings []diagnostic
	failing := false
	decoder := json.NewDecoder(strings.NewReader(run.Stdout))
	for {
		var line communityDiagnostic
		err := decoder.Decode(&line)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, report.Warnings, fmt.Errorf("invalid community linter JSON: %w", err)
		}
		if line.Code == compileCode {
			return nil, report.Warnings, fmt.Errorf("the community linter could not type-check the module: %s", line.Message)
		}
		if line.Message == "" || line.Location.File == "" || line.Location.Line < 1 {
			return nil, report.Warnings, fmt.Errorf("unexpected community diagnostic: %s", run.Stdout)
		}

		finding := diagnostic{Code: line.Code, Message: line.Message, Location: line.Location, Advisory: line.Severity == severityWarning}
		finding.Location.File = strings.TrimPrefix(finding.Location.File, "/src/")
		if index, ok := rules[strings.ToLower(line.Code)]; ok {
			rule := report.Rules[index]
			if rule.Advisory != finding.Advisory {
				return nil, report.Warnings, fmt.Errorf("community finding %s is %s, but its rule's advisory setting is %v", line.Code, line.Severity, rule.Advisory)
			}
			finding.Code, finding.Source, finding.URL = rule.Code, rule.Source, rule.URL
		} else if line.Code != staticcheckCode {
			return nil, report.Warnings, fmt.Errorf("the community linter reported %s, which this check did not select: %s", line.Code, run.Stdout)
		}
		failing = failing || !finding.Advisory
		findings = append(findings, finding)
	}

	if failing != (run.ExitCode == 1) {
		return nil, report.Warnings, fmt.Errorf("community linter exit %d does not match its diagnostics: %s", run.ExitCode, run.Stdout)
	}
	return findings, report.Warnings, nil
}

// codeMixed is the community linter's report of an ignore directive that
// names core and community codes together.
const codeMixed = "lvrules_mixed"

// mergeFindings joins both linters' findings into one report. Where a
// directive mixes core and community codes, each linter sees only its own half
// and reports the other half as unused; lvrules_mixed says what to fix, so
// both unused-directive reports at that position are dropped.
func mergeFindings(core, community []diagnostic) []diagnostic {
	mixed := map[location]bool{}
	for _, finding := range community {
		if finding.Code == codeMixed {
			mixed[location{File: finding.Location.File, Line: finding.Location.Line}] = true
		}
	}

	var merged []diagnostic
	for _, finding := range slices.Concat(core, community) {
		at := location{File: finding.Location.File, Line: finding.Location.Line}
		if finding.Code == staticcheckCode && finding.Message == staleDirective && mixed[at] {
			continue
		}
		merged = append(merged, finding)
	}
	return merged
}

// communityLint runs a check's community rules: build the linter, download
// the consumer's dependencies, and lint with nothing shared. Each step's
// failure is an error naming what failed.
func communityLint(ctx context.Context, source *dagger.Directory, module string, tools toolchain, modules []ruleModule, checks []string, nonce string) ([]diagnostic, []warning, error) {
	warnings, err := releaseNotices(modules)
	if err != nil {
		return nil, nil, err
	}
	pins := make([]buildModule, 0, len(modules))
	for _, module := range modules {
		pins = append(pins, buildModule{Path: module.Path, Version: module.Version, Namespace: module.Namespace})
	}

	built, err := buildCommunityLinter(ctx, tools, pins, nil, nonce)
	if err != nil {
		return nil, warnings, err
	}
	warnings = append(warnings, built.Warnings...)
	deps, err := downloadDependencies(ctx, source, module, tools, nonce)
	if err != nil {
		return nil, warnings, err
	}
	run, err := runCommunityLinter(ctx, source, module, tools, built.Binary, deps, communityConfig{Modules: modules, Checks: checks}, nonce)
	if err != nil {
		return nil, warnings, err
	}

	findings, reported, err := parseCommunity(run)
	return findings, append(warnings, reported...), err
}

// lintOutcome is a whole go-lint check: both linters' findings, warnings, and
// a community failure, which leaves the core findings standing.
type lintOutcome struct {
	Findings []diagnostic
	Warnings []warning
	// Error is why the community rules could not run, or "".
	Error string
}

// failing reports whether any finding fails the check.
func (o lintOutcome) failing() bool {
	return slices.ContainsFunc(o.Findings, func(finding diagnostic) bool { return !finding.Advisory })
}

// report is the JSON goLintReport returns for a passing check.
func (o lintOutcome) report() (string, error) {
	var encoded bytes.Buffer
	err := json.NewEncoder(&encoded).Encode(struct {
		Findings []diagnostic `json:"findings,omitempty"`
		Warnings []warning    `json:"warnings,omitempty"`
	}{o.Findings, o.Warnings})
	return strings.TrimSpace(encoded.String()), err
}

// communitySelfTest builds the fixture rule module into a community linter,
// runs it beside the core linter on the fixture consumer, and checks the
// merged report: the rule's finding with its source and page, a suppressed
// call, and a mixed directive reported once as lvrules_mixed instead of as
// two unused directives. It then lints the vendored fixture, whose dependency
// no proxy serves, on its own and inside a vendored workspace, to prove the
// lint step needs no download for either.
func communitySelfTest(ctx context.Context, fixtures *dagger.Directory, tools toolchain, nonce string) error {
	const fixture = "example.com/lvrules-fixture"
	pins := []buildModule{{Path: fixture, Version: "v0.0.0", Namespace: "fixture", Dir: "/fixtures/rule-module"}}
	built, err := buildCommunityLinter(ctx, tools, pins, fixtures, nonce)
	if err != nil {
		return fmt.Errorf("building the fixture rule module: %w", err)
	}

	consumer := fixtures.Directory("community")
	core, err := lint(ctx, consumer, ".", tools, nonce)
	if err != nil {
		return fmt.Errorf("community fixture must lint for its unused directive, not a tool error: %w", err)
	}
	deps, err := downloadDependencies(ctx, consumer, ".", tools, nonce)
	if err != nil {
		return err
	}
	modules := []ruleModule{{Path: fixture, Version: "v0.0.0", Namespace: "fixture", Select: []string{"fixture_*"}}}
	run, err := runCommunityLinter(ctx, consumer, ".", tools, built.Binary, deps, communityConfig{Modules: modules}, nonce)
	if err != nil {
		return err
	}
	community, _, err := parseCommunity(run)
	if err != nil {
		return fmt.Errorf("community fixture must lint cleanly: %w", err)
	}

	var got []string
	for _, finding := range mergeFindings(core, community) {
		got = append(got, fmt.Sprintf("%s %s:%d %s", finding.Code, finding.Location.File, finding.Location.Line, finding.Source))
	}
	want := []string{"fixture_forbidden community.go:8 " + fixture + "@v0.0.0", "lvrules_mixed community.go:19 "}
	if !slices.Equal(got, want) {
		return fmt.Errorf("community fixture must report exactly %q; got %q", want, got)
	}

	// A vendored module whose dependency no proxy serves lints from vendor/.
	vendored := fixtures.Directory("vendored")
	vendoredDeps, err := downloadDependencies(ctx, vendored, ".", tools, nonce)
	if err != nil {
		return fmt.Errorf("a vendored module needs no download: %w", err)
	}
	vendoredRun, err := runCommunityLinter(ctx, vendored, ".", tools, built.Binary, vendoredDeps, communityConfig{Modules: modules}, nonce)
	if err != nil {
		return err
	}
	if findings, _, err := parseCommunity(vendoredRun); err != nil || len(findings) != 0 {
		return fmt.Errorf("vendored fixture must pass the community linter offline: findings=%v error=%v", findings, err)
	}

	// In a workspace, Go reads only the workspace root's vendor/, the same
	// layout scripts/test-consumers builds for the core linter.
	modulesTxt, err := vendored.File("vendor/modules.txt").Contents(ctx)
	if err != nil {
		return err
	}
	workspace := dag.Directory().
		WithDirectory("app", vendored.WithoutDirectory("vendor")).
		WithDirectory("vendor", vendored.Directory("vendor")).
		WithNewFile("vendor/modules.txt", "## workspace\n"+modulesTxt).
		WithNewFile("go.work", "go "+tools.Go+"\n\nuse ./app\n")
	workspaceDeps, err := downloadDependencies(ctx, workspace, "app", tools, nonce)
	if err != nil {
		return fmt.Errorf("a module in a vendored workspace needs no download: %w", err)
	}
	workspaceRun, err := runCommunityLinter(ctx, workspace, "app", tools, built.Binary, workspaceDeps, communityConfig{Modules: modules}, nonce)
	if err != nil {
		return err
	}
	if findings, _, err := parseCommunity(workspaceRun); err != nil || len(findings) != 0 {
		return fmt.Errorf("workspace-vendored fixture must pass the community linter offline: findings=%v error=%v", findings, err)
	}
	return nil
}
