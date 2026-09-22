package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// testTimeout is go test's own per-package limit, named on the command line so
// no GOFLAGS can lift it. A test that hangs past it panics with a goroutine
// dump and fails its package, which is a finding; goCheckTimeout still bounds
// the whole invocation.
const testTimeout = "10m"

// testArgs is the go-test invocation on either executor. -json lets the check
// tell a failing test from a package that did not build, which the exit code
// cannot. go test's built-in vet subset is off because go-vet owns those
// diagnostics, and go test would otherwise report them as a build failure. A
// fresh run bypasses go test's own result cache as well as Levenshtein's.
// runner/checks.go keeps a copy for the Dagger path; change both together.
func testArgs(fresh bool) []string {
	args := []string{"go", "test", "-race", "-json", "-vet=off", "-timeout=" + testTimeout}
	if fresh {
		args = append(args, "-count=1")
	}
	return append(args, "./...")
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
// events, so a report reads the way a developer's terminal would.
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
// runner/checks.go keeps a copy for the Dagger path; change both together.
func testFindings(module string, exitCode int, stdout, stderr string) ([]finding, error) {
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

	findings := make([]finding, 0, len(failed))
	for _, pkg := range failed {
		findings = append(findings, finding{
			Code:     string(CheckGoTest),
			Message:  packageTranscript(events, pkg),
			Location: location{File: module, Line: 1},
		})
	}
	return findings, nil
}

// goTest runs the target module's tests with the race detector, in the
// workspace the container would see. The race detector needs cgo, so the check
// turns it on and names a missing C compiler up front rather than letting
// every package fail to build runtime/cgo. The report keeps go test's text
// output rather than its event stream.
func (n *Native) goTest(ctx context.Context, req Request, work goRun) ([]finding, toolRun, error) {
	if err := goModule(ctx, req.Target.Dir, work); err != nil {
		return nil, toolRun{}, err
	}
	env := goEnv(work.Env, []string{"CGO_ENABLED=1"})
	if err := raceCompiler(ctx, work.Dir, env); err != nil {
		return nil, toolRun{}, err
	}

	run, err := runTool(ctx, work.Dir, testArgs(req.RerunChecks), env, goCheckTimeout)
	if err != nil {
		return nil, run, err
	}
	findings, err := testFindings(req.Target.Dir, run.ExitCode, run.Stdout, run.Stderr)
	if events, parseErr := testEvents(run.Stdout); parseErr == nil {
		run.Stdout = testTranscript(events)
	}
	return findings, run, err
}

// raceCompiler makes sure the C compiler go would use for cgo is on the
// check's PATH. go test -race cannot build without one, and the pinned
// container always has gcc.
func raceCompiler(ctx context.Context, dir string, env []string) error {
	run, err := runTool(ctx, dir, []string{"go", "env", "CC"}, env, time.Minute)
	if err != nil {
		return err
	}
	if run.ExitCode != 0 {
		return fmt.Errorf("go env CC failed: %s", strings.TrimSpace(run.Stderr))
	}

	compiler := strings.Fields(run.Stdout)
	if len(compiler) == 0 {
		return fmt.Errorf("go-test runs go test -race, which needs cgo, but go env CC names no C compiler")
	}
	if _, err := executable(dir, env, compiler[0]); err != nil {
		return fmt.Errorf("go-test runs go test -race, which needs cgo and a C compiler such as gcc or clang: %w", err)
	}
	return nil
}
