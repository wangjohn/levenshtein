package verify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The runner's shellScript is tested against the same table, so the two
// executors pick the same scripts.
func TestShellScriptMatchesTheSharedTable(t *testing.T) {
	data, err := os.ReadFile("../../runner/testdata/shell-scripts.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		File   string `json:"file"`
		Head   string `json:"head"`
		Script bool   `json:"script"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}

	for _, tc := range cases {
		if got := shellScript(tc.File, []byte(tc.Head)); got != tc.Script {
			t.Errorf("shellScript(%q, %q) = %v, want %v", tc.File, tc.Head, got, tc.Script)
		}
	}
}

func TestShellInputsSkipFixturesAndHonorOneRootConfiguration(t *testing.T) {
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

	files, err := visibleFiles(source, []string{"."}, []string{"excluded"}, sourceSkipDir)
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
	files, err = visibleFiles(source, []string{"."}, nil, sourceSkipDir)
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
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "runner", "testdata", "bad", "run.sh"), "cd /nowhere\n")

	files, err := visibleFiles(source, []string{filepath.Join("runner", "testdata", "bad")}, nil, sourceSkipDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := shellInputs(source, files); err == nil || !strings.Contains(err.Error(), "found no shell scripts") {
		t.Fatalf("a target with only fixture scripts must be an error, not an empty pass: %v", err)
	}
}

func TestShellcheckArgumentsIsolateTheConfiguration(t *testing.T) {
	scripts := []string{"build.sh", "scripts/deploy"}
	common := []string{"shellcheck", "--format=json1", "--severity=warning", "--color=never"}

	if got, want := shellcheckArguments("shellcheck", "", scripts), append(slices.Clone(common), "--norc", "--", "build.sh", "scripts/deploy"); !slices.Equal(got, want) {
		t.Fatalf("without a configuration: %q, want %q", got, want)
	}
	if got, want := shellcheckArguments("shellcheck", ".shellcheckrc", scripts), append(slices.Clone(common), "--rcfile=.shellcheckrc", "--", "build.sh", "scripts/deploy"); !slices.Equal(got, want) {
		t.Fatalf("with a configuration: %q, want %q", got, want)
	}
}

func TestShellEnvDropsHostOptions(t *testing.T) {
	env := shellEnv([]string{"PATH=/bin", "SHELLCHECK_OPTS=--severity=style", "HOME=/home/user"})
	if !slices.Equal(env, []string{"PATH=/bin", "HOME=/home/user"}) {
		t.Fatalf("unexpected ShellCheck environment: %v", env)
	}
}

// This mirrors the runner's test: only an exit that agrees with the json1
// report is a verdict, and every other outcome is a tool error, never a pass.
func TestShellFindingsSeparateFindingsFromToolErrors(t *testing.T) {
	report := `{"comments":[{"file":"run.sh","line":2,"endLine":2,"column":1,"endColumn":8,"level":"warning","code":2164,"message":"Use 'cd ... || exit' or 'cd ... || return' in case cd fails.","fix":null}]}`
	findings, err := shellFindings(1, report, "")
	if err != nil {
		t.Fatal(err)
	}
	want := finding{Code: "SC2164", Message: "Use 'cd ... || exit' or 'cd ... || return' in case cd fails.", Location: location{File: "run.sh", Line: 2, Column: 1}}
	if len(findings) != 1 || findings[0] != want {
		t.Fatalf("findings=%+v, want %+v", findings, want)
	}

	if findings, err := shellFindings(0, `{"comments":[]}`, ""); err != nil || len(findings) != 0 {
		t.Fatalf("a clean run is not a finding: %v %v", findings, err)
	}
	for _, tc := range []struct {
		code   int
		stdout string
		stderr string
	}{
		{2, `{"comments":[]}`, "nope.sh: openBinaryFile: does not exist"},
		{3, "", "Invalid severity"},
		{4, "", "unrecognized option"},
		{1, `{"comments":[]}`, ""},
		{0, report, ""},
		{1, "not json", ""},
		{1, `{"comments":[{"file":"run.sh","line":0,"code":2164,"message":"m"}]}`, ""},
	} {
		if findings, err := shellFindings(tc.code, tc.stdout, tc.stderr); err == nil {
			t.Errorf("exit %d with %q must be an error, got %v", tc.code, tc.stdout, findings)
		}
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
	assertPassReused(t, CheckShellLint)
}
