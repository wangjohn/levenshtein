package checktool

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
)

// testTimeout is go test's own per-package limit, named on the command line so
// no GOFLAGS can lift it. A test that hangs past it panics with a goroutine
// dump and fails its package, which is a finding; each executor still bounds
// the whole invocation.
const testTimeout = "10m"

// TestArgs is the go-test invocation. -json lets the check tell a failing test
// from a package that did not build, which the exit code cannot. go test's
// built-in vet subset is off because go-vet owns those diagnostics, and go test
// would otherwise report them as a build failure. A fresh run bypasses go
// test's own result cache as well as Levenshtein's.
func TestArgs(fresh bool) []string {
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

// TestTranscript is the text go test prints without -json, rebuilt from the
// events it printed with it, so a report reads the way a developer's terminal
// would.
func TestTranscript(stdout string) (string, error) {
	events, err := testEvents(stdout)
	if err != nil {
		return "", err
	}
	return transcript(events), nil
}

func transcript(events []testEvent) string {
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

// TestFindings tells failing tests from everything else. go test exits 1 both
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
func TestFindings(module string, run Run) ([]Finding, error) {
	events, err := testEvents(run.Stdout)
	if err != nil {
		return nil, fmt.Errorf("go test exited %d without readable -json output: %w: %s", run.ExitCode, err, strings.TrimSpace(run.Stderr))
	}
	text := strings.TrimSpace(transcript(events) + "\n" + run.Stderr)
	if run.ExitCode != 0 && run.ExitCode != 1 {
		return nil, fmt.Errorf("go test exited %d: %s", run.ExitCode, text)
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
		return nil, fmt.Errorf("go test could not build %s: %s", strings.Join(broken, ", "), text)
	}
	if len(failed) == 0 {
		if run.ExitCode != 0 {
			return nil, fmt.Errorf("go test exited %d: %s", run.ExitCode, text)
		}
		if !slices.ContainsFunc(events, func(event testEvent) bool { return event.Action == testPass && event.Test != "" }) {
			return nil, fmt.Errorf("module %q ran no tests, or skipped every one; refusing an empty pass: %s", module, text)
		}
		return nil, nil
	}

	findings := make([]Finding, 0, len(failed))
	for _, pkg := range failed {
		findings = append(findings, Finding{
			Code:     string(KindGoTest),
			Message:  packageTranscript(events, pkg),
			Location: Location{File: module, Line: 1},
		})
	}
	return findings, nil
}
