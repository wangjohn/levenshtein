// Shared Go verification with pinned tools and explicit failure propagation.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"dagger/levenshtein/internal/dagger"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

type Levenshtein struct{}

//go:embed toolchain.json
var toolchainJSON []byte

type toolchain struct {
	Go                 string    `json:"go"`
	GoImage            string    `json:"goImage"`
	Staticcheck        string    `json:"staticcheck"`
	StaticcheckRelease string    `json:"staticcheckRelease"`
	Checks             []string  `json:"checks"`
	Zizmor             zizmorPin `json:"zizmor"`
}

type diagnostic struct {
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Location location `json:"location"`
}

type location struct {
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

// linter is the pinned Go image with levenshtein-lint built from this module.
func linter(tools toolchain) *dagger.Container {
	return goContainer(tools).
		WithDirectory("/policy", dag.CurrentModule().Source().Directory("lint")).
		WithWorkdir("/policy").
		WithExec([]string{"go", "build", "-trimpath", "-o", "/go/bin/levenshtein-lint", "./cmd/levenshtein-lint"})
}

func lint(ctx context.Context, source *dagger.Directory, module string, tools toolchain, nonce string) ([]diagnostic, error) {
	if _, err := source.File(path.Join(module, "go.mod")).Contents(ctx); err != nil {
		return nil, fmt.Errorf("module %q needs a readable go.mod: %w", module, err)
	}

	ctr := linter(tools).
		WithDirectory("/src", source).
		WithWorkdir(path.Join("/src", module))

	// Go selects vendor mode for modules/workspaces that use it, and readonly otherwise.
	packages, err := ctr.WithExec([]string{"go", "list", "./..."}).Stdout(ctx)
	if err != nil {
		return nil, fmt.Errorf("discovering packages: %w", err)
	}
	if strings.TrimSpace(packages) == "" {
		return nil, fmt.Errorf("module %q contains no Go packages; refusing an empty pass", module)
	}

	if nonce == "" {
		ctr = ctr.WithMountedCache("/root/.cache/staticcheck", dag.CacheVolume("levenshtein-staticcheck-"+tools.Staticcheck+"-"+tools.Go)).
			WithEnvVariable("STATICCHECK_CACHE", "/root/.cache/staticcheck")
	} else {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce).
			WithEnvVariable("STATICCHECK_CACHE", "/tmp/staticcheck-fresh")
	}
	checked := ctr.WithExec([]string{"/go/bin/levenshtein-lint", "-f=json", "-checks=" + strings.Join(tools.Checks, ","), "./..."}, dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
	exitCode, err := checked.ExitCode(ctx)
	if err != nil {
		return nil, err
	}
	stdout, err := checked.Stdout(ctx)
	if err != nil {
		return nil, err
	}
	stderr, err := checked.Stderr(ctx)
	if err != nil {
		return nil, err
	}
	return parseFindings(exitCode, stdout, stderr, tools.Checks)
}

// lintPattern is one entry of Staticcheck's -checks list: an optional "-",
// then "*", or a rule name or category with an optional trailing "*". The CLI
// validates levenshtein.json with a copy (internal/verify/validation.go); this
// one guards a direct call.
var lintPattern = regexp.MustCompile(`^-?(\*|[A-Za-z][A-Za-z0-9]*\*?)$`)

// selection returns tools with the rule list a go-lint call runs: the shipped
// selection followed by the patterns the caller adds, so the added ones win
// where they overlap. Each added pattern must be well formed, since all of
// them are joined into one -checks flag, and must match a rule the pinned
// linter registers, since a misspelled name would otherwise leave its rule
// silently off. The native executor applies the same rule
// (internal/verify/gotools.go).
func selection(ctx context.Context, tools toolchain, added []string) (toolchain, error) {
	if len(added) == 0 {
		return tools, nil
	}
	for _, check := range added {
		if !lintPattern.MatchString(check) {
			return toolchain{}, fmt.Errorf("go-lint check %q must be one Staticcheck pattern such as \"gocognit\", \"-unparam\", or \"SA5*\"", check)
		}
	}

	listing, err := linter(tools).WithExec([]string{"/go/bin/levenshtein-lint", "-list-checks"}).Stdout(ctx)
	if err != nil {
		return toolchain{}, fmt.Errorf("listing the linter's rules: %w", err)
	}
	if err := registered(added, listing); err != nil {
		return toolchain{}, err
	}
	tools.Checks = append(slices.Clone(tools.Checks), added...)
	return tools, nil
}

// registered checks every pattern against the linter's -list-checks output,
// one rule per line with its name first. internal/verify/gotools.go has a
// copy; both tests load testdata/registered.json.
func registered(patterns []string, listing string) error {
	var names []string
	for line := range strings.Lines(listing) {
		if fields := strings.Fields(line); len(fields) > 0 {
			names = append(names, fields[0])
		}
	}

	for _, pattern := range patterns {
		name := strings.TrimPrefix(pattern, "-")
		if !slices.ContainsFunc(names, func(rule string) bool { return selects(name, rule) }) {
			return fmt.Errorf("go-lint check %q matches no rule levenshtein-lint registers", pattern)
		}
	}
	return nil
}

// allowed reproduces Staticcheck's filterAnalyzerNames (lintcmd/lint.go in
// honnef.co/go/tools v0.8.1) for one code. Patterns apply in order and the
// last one that matches wins, so "all,-SA5001" turns SA5001 off while
// "-SA5001,all" turns it back on. A "-" prefix turns a code off rather than on.
func allowed(checks []string, code string) bool {
	selected := false
	for _, check := range checks {
		pattern := check
		enable := true
		if len(pattern) > 1 && pattern[0] == '-' {
			pattern = pattern[1:]
			enable = false
		}
		if selects(pattern, code) {
			selected = enable
		}
	}
	return selected
}

// selects matches one pattern the way Staticcheck does, ignoring case: "all"
// or "*" matches every code, a trailing "*" after letters matches that exact
// category (S* matches S1002 but not SA5001), a trailing "*" after a digit is a
// plain prefix (SA5* matches SA5001), and anything else is a literal name.
func selects(pattern, code string) bool {
	pattern = strings.ToLower(pattern)
	code = strings.ToLower(code)

	//lint:ignore LV1001 patterns are free-form user input; these are two spellings of one wildcard, not an enum.
	if pattern == "*" || pattern == "all" {
		return true
	}
	prefix, glob := strings.CutSuffix(pattern, "*")
	if !glob {
		return pattern == code
	}
	if strings.IndexFunc(prefix, unicode.IsNumber) != -1 {
		return strings.HasPrefix(code, prefix)
	}
	category := code
	if digit := strings.IndexFunc(code, unicode.IsNumber); digit != -1 {
		category = code[:digit]
	}
	return category == prefix
}

func parseFindings(exitCode int, stdout, stderr string, checks []string) ([]diagnostic, error) {
	if exitCode != 0 && exitCode != 1 {
		return nil, fmt.Errorf("the linter exited %d: %s\n%s", exitCode, stderr, stdout)
	}

	var findings []diagnostic
	decoder := json.NewDecoder(strings.NewReader(stdout))
	for {
		var finding diagnostic
		err := decoder.Decode(&finding)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("invalid linter JSON: %w", err)
		}
		if !allowed(checks, finding.Code) || finding.Message == "" || finding.Location.File == "" || finding.Location.Line < 1 {
			return nil, fmt.Errorf("unexpected diagnostic (possibly a compile error): %s", stdout)
		}
		finding.Location.File = strings.TrimPrefix(finding.Location.File, "/src/")
		findings = append(findings, finding)
	}

	if (exitCode == 0 && len(findings) != 0) || (exitCode == 1 && len(findings) == 0) {
		return nil, fmt.Errorf("linter exit %d does not match diagnostics: %s\n%s", exitCode, stdout, stderr)
	}
	if strings.TrimSpace(stderr) != "" {
		return nil, fmt.Errorf("the linter could not produce a clean result: %s", stderr)
	}
	return findings, nil
}

// expectedBadCodes is the contract that every default rule really runs: the bad
// fixture carries one triggering case per code, so a rule that stops being
// registered or stops firing fails the self-test instead of passing silently.
// scripts/test-checks asserts the same list without Dagger.
var expectedBadCodes = []string{
	"SA4006", "SA5001", "SA5003", "SA9001", "S1002", "ST1005", "QF1011", "U1000",
	"bodyclose", "sqlclosecheck", "rowserrcheck", "noctx", "contextcheck",
	"errcheck", "exhaustive", "nilness", "unusedwrite", "errorlint", "nilerr", "durationcheck", "reassign", "wastedassign", "musttag", "recvcheck", "nilnesserr", "fatcontext",
	"appendAssign", "argOrder", "badCall", "badCond", "badRegexp", "codegenComment", "deprecatedComment", "dupArg", "dupBranchBody", "dupCase", "exitAfterDefer", "filepathJoin", "flagDeref", "flagName", "mapKey", "offBy1",
	"zerologlint", "loggercheck",
	"bidichk", "gocheckcompilerdirectives",
	"unparam",
	"intrange", "usestdlibvars", "perfsprint", "predeclared", "errname", "exptostd",
	"minmax", "mapsloop", "slicescontains", "stringscutprefix", "stringsseq",
	"thelper", "tparallel", "testifylint", "usetesting",
	"LV1001", "LV1002", "LV1003", "LV1004", "LV1005", "LV1006",
}

func (m *Levenshtein) selfTest(ctx context.Context, tools toolchain, nonce string) error {
	fixtures := dag.CurrentModule().Source().Directory("testdata")
	for _, name := range []string{"good", "vendored", "embedded", "modernize-legacy", "complexity"} {
		findings, err := lint(ctx, fixtures.Directory(name), ".", tools, nonce)
		if err != nil || len(findings) != 0 {
			return fmt.Errorf("%s fixture must pass: findings=%v error=%v", name, findings, err)
		}
	}

	// A check that adds gocognit to the shipped selection reports the fixture
	// the default passes, and a misspelled rule is refused rather than ignored.
	opted, err := selection(ctx, tools, []string{"gocognit"})
	if err != nil {
		return fmt.Errorf("adding gocognit to the selection: %w", err)
	}
	complexity, err := lint(ctx, fixtures.Directory("complexity"), ".", opted, nonce)
	if err != nil || len(complexity) != 1 || complexity[0].Code != "gocognit" {
		return fmt.Errorf("complexity fixture must fail for one gocognit finding when a check adds it: findings=%v error=%v", complexity, err)
	}
	if _, err := selection(ctx, tools, []string{"gocogint"}); err == nil || !strings.Contains(err.Error(), "matches no rule") {
		return fmt.Errorf("a misspelled added rule must be refused; got %v", err)
	}

	bad, err := lint(ctx, fixtures.Directory("bad"), ".", tools, nonce)
	if err != nil {
		return fmt.Errorf("bad fixture must fail for its lint diagnostics, not a tool error: %w", err)
	}

	counts := map[string]int{}
	for _, finding := range bad {
		counts[finding.Code]++
	}
	for _, check := range expectedBadCodes {
		if counts[check] < 1 {
			return fmt.Errorf("bad fixture must produce a %s diagnostic; got %v", check, counts)
		}
	}

	for _, fixture := range []struct {
		name    string
		message string
	}{
		{"broken", "undefined: undefinedFunction"},
		{"empty", "contains no Go packages"},
	} {
		_, err := lint(ctx, fixtures.Directory(fixture.name), ".", tools, nonce)
		if err == nil || !strings.Contains(err.Error(), fixture.message) {
			return fmt.Errorf("%s fixture must fail for %q; got %v", fixture.name, fixture.message, err)
		}
	}
	if err := modSelfTest(ctx, fixtures, tools, nonce); err != nil {
		return err
	}
	if err := workflowSecuritySelfTest(ctx, fixtures, tools, nonce); err != nil {
		return err
	}
	if err := goTestSelfTest(ctx, fixtures, tools, nonce); err != nil {
		return err
	}
	if err := gocheckSelfTest(ctx, fixtures, tools, nonce); err != nil {
		return err
	}
	return mutationSelfTest(ctx, fixtures.Directory("mutation"), tools, nonce)
}

// modSelfTest proves go-mod passes a tidy module and fails an untidy one with
// tidy's own diff, not a tool error. Both fixtures resolve without a module
// proxy.
func modSelfTest(ctx context.Context, fixtures *dagger.Directory, tools toolchain, nonce string) error {
	tidy, err := goMod(ctx, fixtures.Directory("mod-tidy"), ".", tools, nonce)
	if err != nil || len(tidy) != 0 {
		return fmt.Errorf("mod-tidy fixture must pass go-mod: findings=%v error=%v", tidy, err)
	}

	untidy, err := goMod(ctx, fixtures.Directory("mod-untidy"), ".", tools, nonce)
	if err != nil {
		return fmt.Errorf("mod-untidy fixture must fail for its diff, not a tool error: %w", err)
	}
	if len(untidy) != 1 || untidy[0].Code != string(checkMod) || !strings.Contains(untidy[0].Message, "-require example.com/mod-untidy/unused v0.0.0") {
		return fmt.Errorf("mod-untidy fixture must report tidy's diff: %v", untidy)
	}
	return nil
}

// workflowSecuritySelfTest proves the pinned zizmor downloads, verifies and
// runs, passes a workflow with nothing to report and fails one with a
// high-severity template injection, keeping zizmor's own report.
func workflowSecuritySelfTest(ctx context.Context, fixtures *dagger.Directory, tools toolchain, nonce string) error {
	secure, err := workflowSecurity(ctx, fixtures.Directory("workflow-secure"), ".", tools, nonce)
	if err != nil || len(secure) != 0 {
		return fmt.Errorf("workflow-secure fixture must pass workflow-security: findings=%v error=%v", secure, err)
	}

	insecure, err := workflowSecurity(ctx, fixtures.Directory("workflow-insecure"), ".", tools, nonce)
	if err != nil {
		return fmt.Errorf("workflow-insecure fixture must fail for its finding, not a tool error: %w", err)
	}
	if len(insecure) != 1 || insecure[0].Code != string(checkWorkflowSecurity) || !strings.Contains(insecure[0].Message, "template-injection") {
		return fmt.Errorf("workflow-insecure fixture must report zizmor's template-injection finding: %v", insecure)
	}
	return nil
}

// goTestSelfTest proves the pinned image can run go test -race, that a failing
// test and a data race are findings with go test's own output, and that a test
// that does not compile is an error rather than a finding.
func goTestSelfTest(ctx context.Context, fixtures *dagger.Directory, tools toolchain, nonce string) error {
	passing, err := goTest(ctx, fixtures.Directory("test-pass"), ".", tools, nonce)
	if err != nil || len(passing) != 0 {
		return fmt.Errorf("test-pass fixture must pass go-test: findings=%v error=%v", passing, err)
	}

	for _, fixture := range []struct {
		name    string
		message string
	}{
		{"test-fail", "Add(2, 2) = 4, want 5"},
		{"test-race", "WARNING: DATA RACE"},
	} {
		findings, err := goTest(ctx, fixtures.Directory(fixture.name), ".", tools, nonce)
		if err != nil {
			return fmt.Errorf("%s fixture must fail for its test, not a tool error: %w", fixture.name, err)
		}
		if len(findings) != 1 || findings[0].Code != string(checkTest) || !strings.Contains(findings[0].Message, fixture.message) {
			return fmt.Errorf("%s fixture must report go test's output: %v", fixture.name, findings)
		}
	}

	broken, err := goTest(ctx, fixtures.Directory("test-build"), ".", tools, nonce)
	if err == nil || !strings.Contains(err.Error(), "go test could not build example.com/test-build") {
		return fmt.Errorf("test-build fixture must be an error, not a finding: findings=%v error=%v", broken, err)
	}
	return nil
}

// mutationSelfTest runs real gremlins on three fixtures and checks the exact
// outcome of each, so a verdict that fails for the wrong reason still fails
// the self-test.
func mutationSelfTest(ctx context.Context, fixtures *dagger.Directory, tools toolchain, nonce string) error {
	strong, err := mutate(ctx, fixtures.Directory("strong"), ".", tools, []string{"add.go"}, nil, defaultAcceptedPath, "", nonce)
	if err != nil || len(strong.Findings) != 0 || strong.Summary.Killed != 1 {
		return fmt.Errorf("mutation/strong must pass with one killed mutant: %+v %v", strong, err)
	}

	accepted, err := mutate(ctx, fixtures.Directory("accepted"), ".", tools, []string{"clamp.go"}, nil, defaultAcceptedPath, "", nonce)
	if err != nil || len(accepted.Findings) != 0 || accepted.Summary.Accepted != 2 {
		return fmt.Errorf("mutation/accepted must pass with both boundary survivors accepted: %+v %v", accepted, err)
	}

	weak, err := mutate(ctx, fixtures.Directory("weak"), ".", tools, []string{"clamp.go"}, nil, defaultAcceptedPath, "", nonce)
	if err != nil {
		return fmt.Errorf("mutation/weak must fail for its survivors, not a tool error: %w", err)
	}
	var survivors []string
	for _, finding := range weak.Findings {
		survivors = append(survivors, fmt.Sprintf("%s %s:%d", finding.Code, finding.Location.File, finding.Location.Line))
	}
	want := []string{"go-mutation clamp.go:5", "go-mutation clamp.go:8"}
	if !slices.Equal(survivors, want) || weak.Incomplete || weak.Summary.NotCovered != 0 {
		return fmt.Errorf("mutation/weak must report exactly %v, with limits/ excluded; got %v (summary %+v)", want, survivors, weak.Summary)
	}

	// With only line 5 changed, the line-8 survivor is reported, not failed.
	scoped, err := mutate(ctx, fixtures.Directory("weak"), ".", tools, []string{"clamp.go"}, map[string][]lineRange{"clamp.go": {{Start: 5, End: 5}}}, defaultAcceptedPath, "", nonce)
	if err != nil {
		return fmt.Errorf("mutation/weak with changed lines must not be a tool error: %w", err)
	}
	if len(scoped.Findings) != 1 || scoped.Findings[0].Location.Line != 5 || scoped.Summary.Unchanged != 1 || scoped.Summary.UnchangedList[0].Line != 8 {
		return fmt.Errorf("mutation/weak limited to line 5 must fail only there and list line 8 as unchanged; got %+v", scoped)
	}
	return nil
}

// GoLint runs the shared Go policy and cleanup rules.
// +check
func (m *Levenshtein) GoLint(ctx context.Context,
	// +optional
	// +defaultPath="/"
	// +ignore=["**/.env", "**/.env.*", "!**/.env.example", "**/.git"]
	source *dagger.Directory,
	// +default="."
	module string,
	// +optional
	nonce string,
	// Staticcheck -checks patterns applied after the shipped selection, such as
	// "gocognit" to turn an opt-in rule on or "-unparam" to turn one off.
	// +optional
	checks []string,
) error {
	if !filepath.IsLocal(module) || path.Clean(module) != module || strings.Contains(module, "\\") {
		return fmt.Errorf("invalid module path %q", module)
	}

	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		return err
	}
	tools, err := selection(ctx, tools, checks)
	if err != nil {
		return err
	}

	findings, err := lint(ctx, source, module, tools, nonce)
	if err != nil {
		return err
	}
	if len(findings) != 0 {
		return &gqlerror.Error{Message: "Go policy lint failed", Extensions: map[string]any{"levenshteinFindings": findings}}
	}
	return nil
}

// SelfTest checks the shared lint rules against good and bad examples.
// +check
func (m *Levenshtein) SelfTest(ctx context.Context,
	// +optional
	nonce string,
) error {
	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		return err
	}
	return m.selfTest(ctx, tools, nonce)
}
