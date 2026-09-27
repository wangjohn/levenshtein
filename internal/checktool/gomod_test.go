package checktool

import (
	"slices"
	"strings"
	"testing"
)

// go mod tidy -diff and go mod verify exit 1 for a mismatch and for a failure
// alike, so only their output can tell an untidy or tampered module from a
// proxy outage, and the outage must never pass.
func TestModFindingsSeparateMismatchesFromToolErrors(t *testing.T) {
	const diff = "diff current/go.sum tidy/go.sum\n--- current/go.sum\n+++ tidy/go.sum\n@@ -1 +0,0 @@\n-golang.org/x/text v0.3.0 h1:abc="
	for _, tc := range []struct {
		name      string
		step      ModStep
		code      int
		stdout    string
		stderr    string
		wantError bool
	}{
		{name: "untidy manifests", step: ModTidy, code: 1, stdout: diff},
		{name: "untidy with download noise", step: ModTidy, code: 1, stdout: diff, stderr: "go: downloading golang.org/x/text v0.3.0"},
		{name: "checksum mismatch", step: ModTidy, code: 1, stderr: "verifying golang.org/x/text@v0.3.0: checksum mismatch\n\tdownloaded: h1:abc=\n\tgo.sum:     h1:def=\n\nSECURITY ERROR\nThis download does NOT match an earlier download recorded in go.sum."},
		{name: "modified download", step: ModVerify, code: 1, stderr: "golang.org/x/text v0.3.0: dir has been modified (/go/pkg/mod/golang.org/x/text@v0.3.0)"},
		{name: "modified zip", step: ModVerify, code: 1, stderr: "golang.org/x/text v0.3.0: zip has been modified (/go/pkg/mod/cache/download/golang.org/x/text/@v/v0.3.0.zip)"},
		{name: "missing ziphash", step: ModVerify, code: 1, stderr: "golang.org/x/text v0.3.0: missing ziphash: open hash file: no such file"},
		{name: "unreachable proxy on tidy", step: ModTidy, code: 1, stderr: `go: example.com/app imports example.invalid/dep: unrecognized import path "example.invalid/dep": https fetch: Bad Gateway`, wantError: true},
		{name: "unreachable proxy on verify", step: ModVerify, code: 1, stderr: `go: example.invalid/dep@v1.0.0: unrecognized import path "example.invalid/dep"`, wantError: true},
		{name: "verify diff output is not tidy's", step: ModVerify, code: 1, stdout: diff, wantError: true},
		{name: "killed", step: ModTidy, code: 137, stdout: diff, wantError: true},
		{name: "silent failure", step: ModVerify, code: 1, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings, err := ModFindings(tc.step, "app", Run{ExitCode: tc.code, Stdout: tc.stdout, Stderr: tc.stderr})
			if (err != nil) != tc.wantError {
				t.Fatalf("findings=%v error=%v", findings, err)
			}
			if tc.wantError {
				return
			}

			want := strings.TrimSpace(tc.stdout + "\n" + tc.stderr)
			if len(findings) != 1 || findings[0].Code != string(KindGoMod) || findings[0].Message != want || findings[0].Location.File != "app" {
				t.Fatalf("lost the go command's output: %+v", findings)
			}
		})
	}

	for _, step := range ModSteps {
		if findings, err := ModFindings(step, "app", Run{Stdout: "all modules verified"}); err != nil || len(findings) != 0 {
			t.Fatalf("%s: a clean run is not a finding: %v %v", step, findings, err)
		}
	}
}

// Tidy runs before verify, each as its own go mod command.
func TestModStepsRunTidyThenVerify(t *testing.T) {
	var got [][]string
	for _, step := range ModSteps {
		got = append(got, step.Args())
	}

	want := [][]string{{"go", "mod", "tidy", "-diff"}, {"go", "mod", "verify"}}
	if !slices.EqualFunc(got, want, slices.Equal) {
		t.Fatalf("steps run %q, want %q", got, want)
	}
}
