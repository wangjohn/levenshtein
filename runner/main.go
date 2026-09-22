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
	Go                 string   `json:"go"`
	GoImage            string   `json:"goImage"`
	Staticcheck        string   `json:"staticcheck"`
	StaticcheckRelease string   `json:"staticcheckRelease"`
	Checks             []string `json:"checks"`
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

func lint(ctx context.Context, source *dagger.Directory, module string, tools toolchain, nonce string) ([]diagnostic, error) {
	if _, err := source.File(path.Join(module, "go.mod")).Contents(ctx); err != nil {
		return nil, fmt.Errorf("module %q needs a readable go.mod: %w", module, err)
	}

	ctr := goContainer(tools).
		WithDirectory("/policy", dag.CurrentModule().Source().Directory("lint")).
		WithWorkdir("/policy").
		WithExec([]string{"go", "build", "-trimpath", "-o", "/go/bin/levenshtein-lint", "./cmd/levenshtein-lint"}).
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
	"SA5001", "SA5003", "SA9001", "S1002", "ST1005", "QF1011", "U1000",
	"bodyclose", "sqlclosecheck", "rowserrcheck", "noctx", "contextcheck",
	"errcheck", "exhaustive", "nilness", "unusedwrite", "errorlint", "nilerr", "durationcheck", "reassign", "wastedassign", "musttag", "recvcheck", "nilnesserr", "fatcontext",
	"appendAssign", "argOrder", "badCall", "badCond", "badRegexp", "codegenComment", "deprecatedComment", "dupArg", "dupBranchBody", "dupCase", "exitAfterDefer", "filepathJoin", "flagDeref", "flagName", "mapKey", "offBy1",
	"zerologlint", "loggercheck", "sloglint",
	"bidichk", "gocheckcompilerdirectives",
	"unparam",
	"intrange", "usestdlibvars", "perfsprint", "predeclared", "errname", "exptostd",
	"minmax", "mapsloop", "slicescontains", "stringscutprefix", "stringsseq",
	"thelper", "tparallel", "testifylint", "usetesting",
	"LV1001", "LV1002", "LV1003", "LV1004", "LV1005", "LV1006",
}

func (m *Levenshtein) selfTest(ctx context.Context, tools toolchain, nonce string) error {
	fixtures := dag.CurrentModule().Source().Directory("testdata")
	for _, name := range []string{"good", "vendored", "embedded", "modernize-legacy"} {
		findings, err := lint(ctx, fixtures.Directory(name), ".", tools, nonce)
		if err != nil || len(findings) != 0 {
			return fmt.Errorf("%s fixture must pass: findings=%v error=%v", name, findings, err)
		}
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
	return mutationSelfTest(ctx, fixtures.Directory("mutation"), tools, nonce)
}

// mutationSelfTest runs real gremlins on three fixtures and checks the exact
// outcome of each, so a verdict that fails for the wrong reason still fails
// the self-test.
func mutationSelfTest(ctx context.Context, fixtures *dagger.Directory, tools toolchain, nonce string) error {
	strong, err := mutate(ctx, fixtures.Directory("strong"), ".", tools, []string{"add.go"}, defaultAcceptedPath, "", nonce)
	if err != nil || len(strong.Findings) != 0 || strong.Summary.Killed != 1 {
		return fmt.Errorf("mutation/strong must pass with one killed mutant: %+v %v", strong, err)
	}

	accepted, err := mutate(ctx, fixtures.Directory("accepted"), ".", tools, []string{"clamp.go"}, defaultAcceptedPath, "", nonce)
	if err != nil || len(accepted.Findings) != 0 || accepted.Summary.Accepted != 2 {
		return fmt.Errorf("mutation/accepted must pass with both boundary survivors accepted: %+v %v", accepted, err)
	}

	weak, err := mutate(ctx, fixtures.Directory("weak"), ".", tools, []string{"clamp.go"}, defaultAcceptedPath, "", nonce)
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
) error {
	if !filepath.IsLocal(module) || path.Clean(module) != module || strings.Contains(module, "\\") {
		return fmt.Errorf("invalid module path %q", module)
	}

	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
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
