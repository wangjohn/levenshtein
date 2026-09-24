package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// internal/verify's shellScript is tested against the same table, so the two
// executors pick the same scripts.
func TestShellScriptMatchesTheSharedTable(t *testing.T) {
	data, err := os.ReadFile("testdata/shell-scripts.json")
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

// The listing the container runs, run here over the fixtures, names the same
// scripts and configuration the native executor finds: testdata is skipped,
// an extensionless script is found by its shebang, and a Python one is not.
func TestShellListingFindsTheFixtureScripts(t *testing.T) {
	for _, tc := range []struct {
		fixture string
		scripts []string
		config  string
	}{
		{"shell-good", []string{"bin/deploy", "scripts/build.sh"}, ".shellcheckrc"},
		{"shell-bad", []string{"bin/tool", "lib.bash", "run.sh"}, ""},
	} {
		listing := shellListing()
		cmd := exec.CommandContext(t.Context(), listing[0], listing[1:]...)
		cmd.Dir = "testdata/" + tc.fixture
		out, err := cmd.Output()
		if err != nil {
			t.Fatal(err)
		}

		scripts, config, err := shellInputs(string(out))
		if err != nil || !slices.Equal(scripts, tc.scripts) || config != tc.config {
			t.Fatalf("%s: scripts=%v config=%q err=%v, want %v and %q", tc.fixture, scripts, config, err, tc.scripts, tc.config)
		}
	}
}

// The listing is an exec's stdout, which Dagger streams to its progress log
// and traces, so it carries no file contents beyond a shebang line: not the
// start of an extensionless key or token file, and not a script's body. Over
// every shared-table case it still selects exactly what shellScript does.
func TestShellListingPrintsNothingButShebangLines(t *testing.T) {
	const key = "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQ\n"
	const token = "export TOKEN=ghp_R2b8kQpXz9LmN4vT7wY1cA3eF6hJ0sD5gK8u\n" // gitleaks:allow
	dir := t.TempDir()
	for file, contents := range map[string]string{"id_ed25519": key, "bin/tool": "#!/bin/sh\n" + token} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, file)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	out := runShellListing(t, dir)
	if strings.Contains(out, "PRIVATE KEY") || strings.Contains(out, "b3BlbnNzaC1") || strings.Contains(out, "ghp_") {
		t.Fatalf("the listing prints file contents: %q", out)
	}
	if scripts, _, err := shellInputs(out); err != nil || !slices.Equal(scripts, []string{"bin/tool"}) {
		t.Fatalf("scripts=%v err=%v, want [bin/tool]", scripts, err)
	}

	data, err := os.ReadFile("testdata/shell-scripts.json")
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
		dir := t.TempDir()
		file := filepath.Join(dir, filepath.FromSlash(tc.File))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(tc.Head), 0o600); err != nil {
			t.Fatal(err)
		}

		scripts, _, err := shellInputs(runShellListing(t, dir))
		if got := err == nil && slices.Equal(scripts, []string{tc.File}); got != tc.Script {
			t.Errorf("listing %q with %q selects %v (%v), want script=%v", tc.File, tc.Head, scripts, err, tc.Script)
		}
	}
}

func runShellListing(t *testing.T, dir string) string {
	t.Helper()
	listing := shellListing()
	cmd := exec.CommandContext(t.Context(), listing[0], listing[1:]...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestShellInputsRefuseAnEmptyCheckAndTwoConfigurations(t *testing.T) {
	if _, _, err := shellInputs("./README.md\x00\x00"); err == nil {
		t.Fatal("a source without scripts must be an error, not an empty pass")
	}
	if _, _, err := shellInputs("./a.sh\x00\x00./.shellcheckrc\x00\x00./shellcheckrc\x00\x00"); err == nil {
		t.Fatal("two root configurations must be an error")
	}
	if _, _, err := shellInputs("./a.sh\x00"); err == nil {
		t.Fatal("a truncated listing must be an error")
	}
}

// This mirrors internal/verify's test: only an exit that agrees with the json1
// report is a verdict, and every other outcome is a tool error, never a pass.
func TestShellFindingsSeparateFindingsFromToolErrors(t *testing.T) {
	report := `{"comments":[{"file":"run.sh","line":2,"endLine":2,"column":1,"endColumn":8,"level":"warning","code":2164,"message":"Use 'cd ... || exit' or 'cd ... || return' in case cd fails.","fix":null}]}`
	findings, err := shellFindings(1, report, "")
	if err != nil || len(findings) != 1 || findings[0].Code != "SC2164" || findings[0].Location != (location{File: "run.sh", Line: 2, Column: 1}) {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
	if findings, err := shellFindings(0, `{"comments":[]}`, ""); err != nil || len(findings) != 0 {
		t.Fatalf("a clean run is not a finding: %v %v", findings, err)
	}

	for _, tc := range []struct {
		code   int
		stdout string
	}{
		{2, `{"comments":[]}`},
		{3, ""},
		{4, ""},
		{1, `{"comments":[]}`},
		{0, report},
		{1, "not json"},
	} {
		if findings, err := shellFindings(tc.code, tc.stdout, "stderr"); err == nil {
			t.Errorf("exit %d with %q must be an error, got %v", tc.code, tc.stdout, findings)
		}
	}
}

func TestShellcheckArgumentsIsolateTheConfiguration(t *testing.T) {
	if got, want := shellcheckArguments("shellcheck", "", []string{"a.sh"}), []string{"shellcheck", "--format=json1", "--severity=warning", "--color=never", "--norc", "--", "a.sh"}; !slices.Equal(got, want) {
		t.Fatalf("without a configuration: %q, want %q", got, want)
	}
	if got, want := shellcheckArguments("shellcheck", ".shellcheckrc", []string{"a.sh"}), []string{"shellcheck", "--format=json1", "--severity=warning", "--color=never", "--rcfile=.shellcheckrc", "--", "a.sh"}; !slices.Equal(got, want) {
		t.Fatalf("with a configuration: %q, want %q", got, want)
	}
}
