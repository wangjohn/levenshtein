package verify

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestShellInputsSkipFixturesAndHonorOneRootConfiguration(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	for path, content := range map[string]string{
		"build.sh":                   "echo build\n",
		"scripts/deploy":             "#!/usr/bin/env bash\necho deploy\n",
		"scripts/report":             "#!/usr/bin/env python3\nprint('report')\n",
		"scripts/testdata/broken.sh": "cd /nowhere\n",
		"vendor/tool/install.sh":     "cd /nowhere\n",
		"web/node_modules/x/run.sh":  "cd /nowhere\n",
		"excluded/skip.sh":           "cd /nowhere\n",
		".env":                       "#!/bin/sh\n",
		"README.md":                  "# readme\n",
		".shellcheckrc":              "disable=SC2034\n",
		"scripts/.shellcheckrc":      "disable=SC2164\n",
	} {
		writeTestFile(t, filepath.Join(source, filepath.FromSlash(path)), content)
	}

	files, err := visibleFiles(t.Context(), visibleRequest(source, []string{"."}, []string{"excluded"}), sourceSkipDir)
	if err != nil {
		t.Fatal(err)
	}
	scripts, config, err := shellInputs(source, files)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"build.sh", "scripts/deploy"}; !slices.Equal(scripts, want) || config != ".shellcheckrc" {
		t.Fatalf("scripts=%v config=%q, want %v and the root .shellcheckrc", scripts, config, want)
	}

	writeTestFile(t, filepath.Join(source, "shellcheckrc"), "disable=SC2086\n")
	files, err = visibleFiles(t.Context(), visibleRequest(source, []string{"."}, nil), sourceSkipDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := shellInputs(source, files); err == nil || !strings.Contains(err.Error(), "configure only one ShellCheck file") {
		t.Fatalf("two root configurations must be an error: %v", err)
	}
}

// A declared input below a skipped directory is skipped too, as the Dagger
// path's listing prunes it from the root.
func TestShellInputsSkipADeclaredInputUnderTestdata(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "runner", "testdata", "bad", "run.sh"), "cd /nowhere\n")

	files, err := visibleFiles(t.Context(), visibleRequest(source, []string{filepath.Join("runner", "testdata", "bad")}, nil), sourceSkipDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := shellInputs(source, files); err == nil || !strings.Contains(err.Error(), "found no shell scripts") {
		t.Fatalf("a target with only fixture scripts must be an error, not an empty pass: %v", err)
	}
}

func TestShellEnvDropsHostOptions(t *testing.T) {
	t.Parallel()
	env := shellEnv([]string{"PATH=/bin", "SHELLCHECK_OPTS=--severity=style", "HOME=/home/user"})
	if !slices.Equal(env, []string{"PATH=/bin", "HOME=/home/user"}) {
		t.Fatalf("unexpected ShellCheck environment: %v", env)
	}
}

// assertPassReused runs a passing check twice through the result cache on
// each executor and requires the second run to reuse the first, and requires
// an ordinary Dagger call to keep Dagger's own cache.
func assertPassReused(t *testing.T, check CheckKind) {
	t.Helper()
	for _, kind := range []ExecutorKind{ExecutorDagger, ExecutorNative} {
		req := cacheRequest(t)
		req.Check.Kind = check
		req.Environment.Executor = kind
		executor := &countingExecutor{status: StatusPassed}
		runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}

		for range 2 {
			if result := runner.Execute(context.Background(), req); result.Status != StatusPassed {
				t.Fatalf("%s on %s: unexpected result: %+v", check, kind, result)
			}
		}
		if executor.calls != 1 {
			t.Fatalf("%s on %s: want one run and one reuse, got %d calls", check, kind, executor.calls)
		}
	}

	req := cacheRequest(t)
	req.Check.Kind = check
	if executionNonce(req) != "" {
		t.Fatalf("an ordinary %s run should keep Dagger's cache", check)
	}
}

// shell-lint depends only on the declared scripts, the configuration, and the
// pinned ShellCheck, so a pass is reused like workflow-security's.
func TestShellLintPassesAreReused(t *testing.T) {
	t.Parallel()
	assertPassReused(t, CheckShellLint)
}
