//go:build integration

package verify

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"
)

// sourceRequest points a native shared check at any source directory, with
// this repository as the pinned shared checkout.
func sourceRequest(t *testing.T, shared, source string, kind CheckKind) Request {
	t.Helper()
	source, err := filepath.EvalSymlinks(source)
	if err != nil {
		t.Fatal(err)
	}

	return Request{
		Source: source,
		Shared: shared,
		PlannedCheck: PlannedCheck{
			ID:          string(kind),
			Check:       Check{Kind: kind, Target: "app", Environment: "host"},
			Target:      Target{Dir: ".", Workspace: ".", Inputs: []string{"."}, Discovery: DiscoveryFilesystem},
			Environment: Environment{Executor: ExecutorNative},
		},
	}
}

// copyFixture copies one of the runner's fixtures into a temporary directory.
func copyFixture(t *testing.T, shared, fixture string) string {
	t.Helper()
	from := filepath.Join(shared, "runner", "testdata", fixture)
	to := t.TempDir()
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		writeTestFile(t, filepath.Join(to, rel), string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return to
}

func findingLines(findings []finding) []string {
	lines := make([]string, 0, len(findings))
	for _, finding := range findings {
		lines = append(lines, fmt.Sprintf("%s %s:%d:%d", finding.Code, finding.Location.File, finding.Location.Line, finding.Location.Column))
	}
	sort.Strings(lines)
	return lines
}

// The native executor reaches the same verdicts as the Dagger self-test
// (runner/shelllint.go) on the same fixtures. These need github.com for the
// pinned ShellCheck release.
func TestNativeShellLintAgreesWithTheFixtures(t *testing.T) {
	shared := repositoryRoot(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	native := &Native{Cache: &Cache{Dir: t.TempDir()}}

	t.Run("shell-lint passes shell-good", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "shell-good", CheckShellLint))
		if result.Status != StatusPassed {
			t.Fatalf("shell-good fixture must pass shell-lint: %+v", result)
		}
	})

	t.Run("shell-lint fails shell-bad at each diagnostic", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "shell-bad", CheckShellLint))
		if result.Status != StatusFailed {
			t.Fatalf("shell-bad fixture must fail for its diagnostics, not a tool error: %+v", result)
		}
		want := []string{"SC2034 run.sh:3:1", "SC2164 run.sh:2:1", "SC2168 lib.bash:1:1", "SC3010 bin/tool:2:4"}
		if got := findingLines(fixtureFindings(t, result)); !slices.Equal(got, want) {
			t.Fatalf("shell-bad findings %v, want %v", got, want)
		}
	})

	t.Run("shell-lint without its root configuration reports what it disabled", func(t *testing.T) {
		source := copyFixture(t, shared, "shell-good")
		if err := os.Remove(filepath.Join(source, ".shellcheckrc")); err != nil {
			t.Fatal(err)
		}
		// Excluding the configuration has the same effect as deleting it.
		req := sourceRequest(t, shared, copyFixture(t, shared, "shell-good"), CheckShellLint)
		req.Target.Exclude = []string{".shellcheckrc"}
		for _, req := range []Request{sourceRequest(t, shared, source, CheckShellLint), req} {
			result := native.Execute(ctx, req)
			if got := findingLines(fixtureFindings(t, result)); result.Status != StatusFailed || !slices.Equal(got, []string{"SC2034 bin/deploy:3:1"}) {
				t.Fatalf("without .shellcheckrc the unused variable must be reported: %v %+v", got, result)
			}
		}
	})

	t.Run("shell-lint refuses a target without scripts", func(t *testing.T) {
		result := native.Execute(ctx, fixtureRequest(t, shared, "good", CheckShellLint))
		if result.Status != StatusError || !strings.Contains(result.Error, "found no shell scripts") {
			t.Fatalf("nothing to check must error rather than pass: %+v", result)
		}
	})
}
