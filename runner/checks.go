package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"dagger/levenshtein/internal/dagger"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

type checkName string

const (
	checkLint             checkName = "go-lint"
	checkVet              checkName = "go-vet"
	checkMod              checkName = "go-mod"
	checkTest             checkName = "go-test"
	checkHTTP             checkName = "go-http"
	checkSQL              checkName = "go-sql"
	checkVuln             checkName = "go-vuln"
	checkWorkflow         checkName = "workflow-lint"
	checkWorkflowSecurity checkName = "workflow-security"
	checkShellLint        checkName = "shell-lint"
	checkSecrets          checkName = "secrets"
	checkSelfTest         checkName = "self-test"
)

func knownCheck(check checkName) bool {
	switch check {
	case checkLint, checkVet, checkMod, checkTest, checkHTTP, checkSQL, checkVuln, checkWorkflow, checkWorkflowSecurity, checkShellLint, checkSecrets, checkSelfTest:
		return true
	case checkImports, checkGenerate, checkApidiff:
		return false // Each has its own function.
	}
	return false
}

func goContainer(tools toolchain) *dagger.Container {
	return dag.Container().From(tools.GoImage).
		WithEnvVariable("GOTOOLCHAIN", "local").
		WithMountedCache("/go/pkg/mod", dag.CacheVolume("levenshtein-go-mod-"+tools.Go)).
		WithMountedCache("/root/.cache/go-build", dag.CacheVolume("levenshtein-go-build-"+tools.Go))
}

func executeCheck(ctx context.Context, source *dagger.Directory, module string, tools toolchain, check checkName, nonce string) ([]diagnostic, error) {
	//exhaustive:ignore Other checks use standalone tools below.
	switch check {
	case checkLint:
		return lint(ctx, source, module, tools, nonce)
	case checkHTTP, checkSQL:
		checks := map[checkName]string{checkHTTP: "bodyclose", checkSQL: "sqlclosecheck"}
		tools.Checks = []string{checks[check]}
		return lint(ctx, source, module, tools, nonce)
	case checkMod:
		return goMod(ctx, source, module, tools, nonce)
	case checkTest:
		return goTest(ctx, source, module, tools, nonce)
	case checkWorkflowSecurity:
		return workflowSecurity(ctx, source, module, tools, nonce)
	case checkShellLint:
		return shellLint(ctx, source, tools, nonce)
	case checkSecrets:
		return secrets(ctx, source, tools, nonce)
	}

	ctr := goContainer(tools)
	var command []string
	if check == checkVet {
		command = []string{"go", "vet", "./..."}
	} else {
		packages := map[checkName]string{
			checkWorkflow: "github.com/rhysd/actionlint/cmd/actionlint",
			checkVuln:     "golang.org/x/vuln/cmd/govulncheck",
		}
		pkg, ok := packages[check]
		if !ok {
			return nil, fmt.Errorf("unsupported check %q", check)
		}
		ctr = ctr.WithDirectory("/tools", dag.CurrentModule().Source().Directory("tools")).
			WithWorkdir("/tools").
			WithExec([]string{"go", "build", "-trimpath", "-o", "/usr/local/bin/check", pkg})
		command = []string{"/usr/local/bin/check", "./..."}
		if check == checkWorkflow {
			// Shell/Python tools are separate checks, not ambient optional dependencies.
			command = []string{"/usr/local/bin/check", "-shellcheck=", "-pyflakes="}
			var workflows []string
			for _, pattern := range []string{".github/workflows/*.yml", ".github/workflows/*.yaml"} {
				files, err := source.Glob(ctx, pattern)
				if err != nil {
					return nil, err
				}
				workflows = append(workflows, files...)
			}
			if len(workflows) == 0 {
				return nil, fmt.Errorf("workflow-lint requires .github/workflows/*.yml or *.yaml")
			}
			configs, err := source.Glob(ctx, ".github/actionlint.y*ml")
			if err != nil {
				return nil, err
			}
			if len(configs) > 1 {
				return nil, fmt.Errorf("configure only one .github/actionlint YAML file")
			}
			if len(configs) == 1 {
				command = append(command, "-config-file", configs[0])
			}
			sort.Strings(workflows)
			command = append(command, workflows...) // Exported source has no .git for discovery.
		}
	}

	ctr = ctr.WithDirectory("/src", source).WithWorkdir(path.Join("/src", module))
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}
	if check != checkWorkflow {
		if _, err := source.File(path.Join(module, "go.mod")).Contents(ctx); err != nil {
			return nil, fmt.Errorf("module %q needs a readable go.mod: %w", module, err)
		}
		packages, err := ctr.WithExec([]string{"go", "list", "./..."}).Stdout(ctx)
		if err != nil || strings.TrimSpace(packages) == "" {
			return nil, fmt.Errorf("module %q package discovery failed or found no packages: %v", module, err)
		}
	}

	checked := ctr.WithExec(command, dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
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
	return commandFindings(check, module, exitCode, stdout, stderr)
}

// Preserve native tool output, including its precise locations, in the report.
func commandFindings(check checkName, module string, exitCode int, stdout, stderr string) ([]diagnostic, error) {
	if exitCode == 0 {
		return nil, nil
	}
	message := strings.TrimSpace(stdout + "\n" + stderr)
	failureCode := 1
	if check == checkVuln {
		failureCode = 3 // govulncheck distinguishes vulnerabilities from tool errors.
	}
	if exitCode != failureCode || message == "" {
		return nil, fmt.Errorf("%s exited %d: %s", check, exitCode, message)
	}
	return []diagnostic{{
		Code:     string(check),
		Message:  message,
		Location: location{File: module, Line: 1},
	}}, nil
}

// modStep is one of the two go commands a go-mod check runs, in order: whether
// go.mod and go.sum are already what tidy would write, then whether the
// downloaded dependencies still match the hashes go.sum recorded.
type modStep string

const (
	modTidy   modStep = "tidy -diff"
	modVerify modStep = "verify"
)

func (s modStep) args() []string {
	return append([]string{"go", "mod"}, strings.Fields(string(s))...)
}

// goMod checks the module's manifests on their own. Tidy ignores a workspace
// anyway, and with GOWORK=off verify covers this module's requirements rather
// than every workspace member's. Neither command loads packages, so a module
// that only declares tools is still checked. Tidy reads go.mod and go.sum and
// never vendor/, so a vendored module still needs its module proxy.
func goMod(ctx context.Context, source *dagger.Directory, module string, tools toolchain, nonce string) ([]diagnostic, error) {
	if _, err := source.File(path.Join(module, "go.mod")).Contents(ctx); err != nil {
		return nil, fmt.Errorf("module %q needs a readable go.mod: %w", module, err)
	}
	ctr := goContainer(tools).
		WithEnvVariable("GOWORK", "off").
		WithDirectory("/src", source).
		WithWorkdir(path.Join("/src", module))
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}

	var findings []diagnostic
	for _, step := range []modStep{modTidy, modVerify} {
		checked := ctr.WithExec(step.args(), dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
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
		found, err := modFindings(step, module, exitCode, stdout, stderr)
		if err != nil {
			return nil, err
		}
		findings = append(findings, found...)
	}
	return findings, nil
}

// modifiedModule is how go mod verify names a download that no longer matches
// the hash recorded when it was fetched.
var modifiedModule = regexp.MustCompile(`(?m)^\S+ \S+: (zip has been modified|dir has been modified|missing ziphash)`)

// modFindings tells a go-mod step's diagnostics from a tool error. Both
// commands exit 1 either way, so the output decides: tidy -diff prints a diff
// on stdout only when the manifests are untidy, verify names each module whose
// download was modified, and either reports a SECURITY ERROR when a download
// disagrees with go.sum. Anything else, such as an unreachable module proxy, is
// an error and never a pass. internal/verify keeps a copy for the native
// executor; change both together.
func modFindings(step modStep, module string, exitCode int, stdout, stderr string) ([]diagnostic, error) {
	if exitCode == 0 {
		return nil, nil
	}
	message := strings.TrimSpace(stdout + "\n" + stderr)
	mismatch := strings.Contains(stderr, "SECURITY ERROR")
	switch step {
	case modTidy:
		mismatch = mismatch || strings.TrimSpace(stdout) != ""
	case modVerify:
		mismatch = mismatch || modifiedModule.MatchString(stderr)
	}
	if exitCode != 1 || !mismatch {
		return nil, fmt.Errorf("go mod %s exited %d: %s", step, exitCode, message)
	}
	return []diagnostic{{
		Code:     string(checkMod),
		Message:  message,
		Location: location{File: module, Line: 1},
	}}, nil
}

// testTimeout is go test's own per-package limit, named on the command line so
// nothing can lift it. A test that hangs past it panics with a goroutine dump
// and fails its package, which is a finding; the CLI bounds the whole call.
const testTimeout = "10m"

// testArgs is the go-test invocation. -json lets the check tell a failing test
// from a package that did not build, which the exit code cannot. go test's
// built-in vet subset is off because go-vet owns those diagnostics, and go test
// would otherwise report them as a build failure. A fresh run bypasses go
// test's own result cache, which lives in the shared build cache volume.
// internal/verify keeps a copy for the native executor; change both together.
func testArgs(fresh bool) []string {
	args := []string{"go", "test", "-race", "-json", "-vet=off", "-timeout=" + testTimeout}
	if fresh {
		args = append(args, "-count=1")
	}
	return append(args, "./...")
}

// goTest runs the module's tests with the race detector, which needs cgo; the
// pinned golang image carries gcc for it.
func goTest(ctx context.Context, source *dagger.Directory, module string, tools toolchain, nonce string) ([]diagnostic, error) {
	if _, err := source.File(path.Join(module, "go.mod")).Contents(ctx); err != nil {
		return nil, fmt.Errorf("module %q needs a readable go.mod: %w", module, err)
	}
	ctr := goContainer(tools).
		WithEnvVariable("CGO_ENABLED", "1").
		WithDirectory("/src", source).
		WithWorkdir(path.Join("/src", module))
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}
	packages, err := ctr.WithExec([]string{"go", "list", "./..."}).Stdout(ctx)
	if err != nil || strings.TrimSpace(packages) == "" {
		return nil, fmt.Errorf("module %q package discovery failed or found no packages: %v", module, err)
	}

	checked := ctr.WithExec(testArgs(nonce != ""), dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
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
	return testFindings(module, exitCode, stdout, stderr)
}

// testAction is the kind of one go test -json event, as test2json names it.
// Only the actions the check reads are listed.
type testAction string

const (
	testPass        testAction = "pass"
	testSkip        testAction = "skip"
	testFail        testAction = "fail"
	testOutput      testAction = "output"
	testBuildOutput testAction = "build-output"
)

// testEvent is the part of a go test -json event the check reads. A package
// whose test binary could not be built or set up fails with FailedBuild set;
// its compiler output arrives as build-output events. The tags are
// test2json's field names.
type testEvent struct {
	Action      testAction `json:"Action"`
	Package     string     `json:"Package"`
	Test        string     `json:"Test"`
	Output      string     `json:"Output"`
	FailedBuild string     `json:"FailedBuild"`
}

func testEvents(stdout string) ([]testEvent, error) {
	var events []testEvent
	decoder := json.NewDecoder(strings.NewReader(stdout))
	for {
		var event testEvent
		err := decoder.Decode(&event)
		if errors.Is(err, io.EOF) {
			return events, nil
		}
		if err != nil {
			return nil, fmt.Errorf("go test -json printed something other than events: %w", err)
		}
		events = append(events, event)
	}
}

// testTranscript is the text go test prints without -json, rebuilt from the
// events.
func testTranscript(events []testEvent) string {
	var text strings.Builder
	for _, event := range events {
		if event.Action == testOutput || event.Action == testBuildOutput {
			text.WriteString(event.Output)
		}
	}
	return text.String()
}

// packageTranscript is one failed package's output without the tests in it
// that passed or were skipped. Output from a test that never reported a result,
// such as the one running when the package timed out, is kept.
func packageTranscript(events []testEvent, pkg string) string {
	finished := map[string]bool{}
	for _, event := range events {
		if event.Package == pkg && event.Test != "" && (event.Action == testPass || event.Action == testSkip) {
			finished[event.Test] = true
		}
	}

	var text strings.Builder
	for _, event := range events {
		if event.Package == pkg && event.Action == testOutput && !finished[event.Test] {
			text.WriteString(event.Output)
		}
	}
	return strings.TrimSpace(text.String())
}

// testFindings tells failing tests from everything else. go test exits 1 both
// when a test fails and when a package does not build, so the events decide:
// a package that failed with FailedBuild set ([build failed] or [setup failed])
// is an error, since its tests never ran, and so is an exit 1 with no failed
// package at all, such as a module with no packages. Every other package with a
// failed test, or that failed itself, is a finding carrying its own output,
// which covers a failed or panicking test, a data race the race detector
// reported, and a test that hit the timeout. That holds even when go test
// exits 0, which it does when a TestMain drops m.Run's result and exits 0 after
// a test failed. A run where no test passed, because none ran or every one
// skipped, is refused rather than reported as a pass.
// internal/verify keeps a copy for the native executor; change both together.
func testFindings(module string, exitCode int, stdout, stderr string) ([]diagnostic, error) {
	events, err := testEvents(stdout)
	if err != nil {
		return nil, fmt.Errorf("go test exited %d without readable -json output: %w: %s", exitCode, err, strings.TrimSpace(stderr))
	}
	transcript := strings.TrimSpace(testTranscript(events) + "\n" + stderr)
	if exitCode != 0 && exitCode != 1 {
		return nil, fmt.Errorf("go test exited %d: %s", exitCode, transcript)
	}

	var failed, broken []string
	for _, event := range events {
		if event.Action != testFail {
			continue
		}
		if event.FailedBuild != "" {
			if !slices.Contains(broken, event.Package) {
				broken = append(broken, event.Package)
			}
		} else if !slices.Contains(failed, event.Package) {
			failed = append(failed, event.Package)
		}
	}
	if len(broken) != 0 {
		return nil, fmt.Errorf("go test could not build %s: %s", strings.Join(broken, ", "), transcript)
	}
	if len(failed) == 0 {
		if exitCode != 0 {
			return nil, fmt.Errorf("go test exited %d: %s", exitCode, transcript)
		}
		if !slices.ContainsFunc(events, func(event testEvent) bool { return event.Action == testPass && event.Test != "" }) {
			return nil, fmt.Errorf("module %q ran no tests, or skipped every one; refusing an empty pass: %s", module, transcript)
		}
		return nil, nil
	}

	findings := make([]diagnostic, 0, len(failed))
	for _, pkg := range failed {
		findings = append(findings, diagnostic{
			Code:     string(checkTest),
			Message:  packageTranscript(events, pkg),
			Location: location{File: module, Line: 1},
		})
	}
	return findings, nil
}

// SharedCheck runs one pinned upstream check for a target.
func (m *Levenshtein) SharedCheck(ctx context.Context,
	// +defaultPath="/"
	// +ignore=["**/.env", "**/.env.*", "!**/.env.example", "**/.git"]
	source *dagger.Directory,
	check string,
	// +default="."
	module string,
	// +optional
	nonce string,
) error {
	kind := checkName(check)
	if !knownCheck(kind) || kind == checkSelfTest {
		return fmt.Errorf("unsupported shared check %q", check)
	}
	if !filepath.IsLocal(module) || path.Clean(module) != module || strings.Contains(module, "\\") {
		return fmt.Errorf("invalid module path %q", module)
	}
	if (kind == checkWorkflow || kind == checkWorkflowSecurity || kind == checkShellLint || kind == checkSecrets) && module != "." {
		return fmt.Errorf("%s requires a repository-root target", kind)
	}
	// Their verdicts depend on state no source input covers, so Dagger must
	// never answer them from its own cache.
	if (kind == checkVuln || kind == checkMod) && nonce == "" {
		return fmt.Errorf("%s requires a unique nonce; use the Levenshtein CLI", kind)
	}

	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		return err
	}
	findings, err := executeCheck(ctx, source, module, tools, kind, nonce)
	if err != nil {
		return err
	}
	if len(findings) != 0 {
		return &gqlerror.Error{Message: "shared check failed", Extensions: map[string]any{"levenshteinFindings": findings}}
	}
	return nil
}
