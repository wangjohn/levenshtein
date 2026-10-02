package checktool

import (
	"slices"
	"testing"
)

func TestShellScriptMatchesTheSharedTable(t *testing.T) {
	cases := sharedTable[[]struct {
		File   string `json:"file"`
		Head   string `json:"head"`
		Script bool   `json:"script"`
	}](t, "shell-scripts.json")
	if len(cases) == 0 {
		t.Fatal("runner/testdata/shell-scripts.json has no cases")
	}

	for _, tc := range cases {
		if got := ShellScript(tc.File, []byte(tc.Head)); got != tc.Script {
			t.Errorf("ShellScript(%q, %q) = %v, want %v", tc.File, tc.Head, got, tc.Script)
		}
	}
}

func TestShellcheckArgumentsIsolateTheConfiguration(t *testing.T) {
	scripts := []string{"build.sh", "scripts/deploy"}
	common := []string{"shellcheck", "--format=json1", "--severity=warning", "--color=never"}

	if got, want := ShellcheckArguments("shellcheck", "", scripts), append(slices.Clone(common), "--norc", "--", "build.sh", "scripts/deploy"); !slices.Equal(got, want) {
		t.Fatalf("without a configuration: %q, want %q", got, want)
	}
	if got, want := ShellcheckArguments("shellcheck", ".shellcheckrc", scripts), append(slices.Clone(common), "--rcfile=.shellcheckrc", "--", "build.sh", "scripts/deploy"); !slices.Equal(got, want) {
		t.Fatalf("with a configuration: %q, want %q", got, want)
	}
}

// Only an exit that agrees with the json1 report is a verdict, and every other
// outcome is a tool error, never a pass.
func TestShellFindingsSeparateFindingsFromToolErrors(t *testing.T) {
	report := `{"comments":[{"file":"run.sh","line":2,"endLine":2,"column":1,"endColumn":8,"level":"warning","code":2164,"message":"Use 'cd ... || exit' or 'cd ... || return' in case cd fails.","fix":null}]}`
	findings, err := ShellFindings(Run{ExitCode: 1, Stdout: report})
	if err != nil {
		t.Fatal(err)
	}
	want := Finding{Code: "SC2164", Message: "Use 'cd ... || exit' or 'cd ... || return' in case cd fails.", Location: Location{File: "run.sh", Line: 2, Column: 1}}
	if len(findings) != 1 || findings[0] != want {
		t.Fatalf("findings=%+v, want %+v", findings, want)
	}

	// ShellCheck reports a missing or unknown shebang on line 1.
	firstLine := `{"comments":[{"file":"run","line":1,"endLine":1,"column":1,"endColumn":1,"level":"error","code":2148,"message":"Tips depend on target shell and yours is unknown.","fix":null}]}`
	if findings, err := ShellFindings(Run{ExitCode: 1, Stdout: firstLine}); err != nil || len(findings) != 1 || findings[0].Location.Line != 1 {
		t.Fatalf("a diagnostic on the first line is a finding there: %v %v", findings, err)
	}

	if findings, err := ShellFindings(Run{Stdout: `{"comments":[]}`}); err != nil || len(findings) != 0 {
		t.Fatalf("a clean run is not a finding: %v %v", findings, err)
	}
	for _, tc := range []Run{
		{Stdout: `null`},
		{Stdout: `{}`},
		{Stdout: `{"comments":null}`},
		{ExitCode: 2, Stdout: `{"comments":[]}`, Stderr: "nope.sh: openBinaryFile: does not exist"},
		{ExitCode: 3, Stderr: "Invalid severity"},
		{ExitCode: 4, Stderr: "unrecognized option"},
		{ExitCode: 1, Stdout: `{"comments":[]}`},
		{ExitCode: 0, Stdout: report},
		{ExitCode: 1, Stdout: "not json"},
		{ExitCode: 1, Stdout: `{"comments":[{"file":"run.sh","line":0,"code":2164,"message":"m"}]}`},
	} {
		if findings, err := ShellFindings(tc); err == nil {
			t.Errorf("exit %d with %q must be an error, got %v", tc.ExitCode, tc.Stdout, findings)
		}
	}
}
