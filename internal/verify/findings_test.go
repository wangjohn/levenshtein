package verify

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This mirrors the Dagger runner's own parseFindings test. The two
// implementations must reach the same verdict on the same tool output, so when
// one changes the other has to change with it.
func TestLintExitStatusAndDiagnosticsAgree(t *testing.T) {
	valid := `{"code":"SA5001","message":"defer before error check","location":{"file":"/src/close.go","line":7,"column":2}}`

	findings, err := parseFindings(1, valid, "", []string{"SA5001"}, "/src")
	if err != nil || len(findings) != 1 || findings[0].Location.File != "close.go" {
		t.Fatalf("lost lint diagnostic: %v, %v", findings, err)
	}

	for _, tc := range []struct {
		code   int
		out    string
		stderr string
	}{
		{0, valid, ""}, {1, "", ""}, {2, "", "crash"},
		{1, "not JSON", ""}, {0, "", "warning: no packages"},
		{1, `{"code":"compile","message":"syntax error"}`, ""},
	} {
		if _, err := parseFindings(tc.code, tc.out, tc.stderr, []string{"SA5001"}, "/src"); err == nil {
			t.Errorf("accepted invalid tool result: %+v", tc)
		}
	}
	if _, err := parseFindings(0, "", "", []string{"SA5001"}, "/src"); err != nil {
		t.Fatal(err)
	}
}

func TestStaticcheckWildcardDoesNotAcceptCompilerErrors(t *testing.T) {
	for _, code := range []string{"SA4006", "compile"} {
		output := `{"code":"` + code + `","message":"finding","location":{"file":"/src/a.go","line":1}}`
		if _, err := parseFindings(1, output, "", []string{"SA*"}, "/src"); (err == nil) != (code == "SA4006") {
			t.Fatalf("unexpected result for %s: %v", code, err)
		}
	}
}

// selectionCase is one row of runner/testdata/selection.json.
type selectionCase struct {
	Name   string   `json:"name"`
	Checks []string `json:"checks"`
	Code   string   `json:"code"`
	Want   bool     `json:"want"`
}

// This copy of the check filter must select exactly what the runner's does, so
// both load one table rather than each keeping its own list of cases.
func TestCheckSelectionMatchesTheRunner(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "runner", "testdata", "selection.json"))
	if err != nil {
		t.Fatal(err)
	}
	var shared struct {
		Cases []selectionCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &shared); err != nil {
		t.Fatal(err)
	}
	if len(shared.Cases) == 0 {
		t.Fatal("runner/testdata/selection.json has no cases")
	}

	for _, tc := range shared.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			if got := allowed(tc.Checks, tc.Code); got != tc.Want {
				t.Fatalf("allowed(%v, %q) = %v, want %v", tc.Checks, tc.Code, got, tc.Want)
			}
		})
	}
}

// gocognit and deferInLoop are compiled into the linter but opt-in, so the
// shipped selection a native go-lint reads must keep them off while leaving
// the rest on.
func TestShippedSelectionLeavesOptInRulesOff(t *testing.T) {
	checks, err := sharedChecks(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	for _, code := range []string{"gocognit", "deferInLoop"} {
		if allowed(checks, code) {
			t.Errorf("the shipped selection %v reports %s", checks, code)
		}
	}
	if !allowed(checks, "errcheck") {
		t.Errorf("the shipped selection %v drops errcheck", checks)
	}
}

// registeredCase is one row of runner/testdata/registered.json.
type registeredCase struct {
	Name    string   `json:"name"`
	Checks  []string `json:"checks"`
	Matches bool     `json:"matches"`
}

// A pattern a go-lint check adds must match a rule the linter lists, and this
// copy must agree with the runner's, so both load one table.
func TestAddedLintChecksMatchTheRunner(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "runner", "testdata", "registered.json"))
	if err != nil {
		t.Fatal(err)
	}
	var shared struct {
		Listing string           `json:"listing"`
		Cases   []registeredCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &shared); err != nil {
		t.Fatal(err)
	}
	if len(shared.Cases) == 0 {
		t.Fatal("runner/testdata/registered.json has no cases")
	}

	for _, tc := range shared.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			if err := registered(tc.Checks, shared.Listing); (err == nil) != tc.Matches {
				t.Fatalf("registered(%v) = %v, want matches=%v", tc.Checks, err, tc.Matches)
			}
		})
	}
}

func TestCommandFailuresDoNotBecomePassingResults(t *testing.T) {
	for _, tc := range []struct {
		check     CheckKind
		code      int
		output    string
		wantError bool
	}{
		{check: CheckGoVet, code: 1, output: "copylocks: copies lock value"},
		{check: CheckWorkflowLint, code: 1, output: "unknown job"},
		{check: CheckGoVuln, code: 3, output: "reachable vulnerability"},
		{check: CheckGoVuln, code: 1, output: "database unavailable", wantError: true},
		{check: CheckWorkflowLint, code: 3, output: "configuration error", wantError: true},
		{check: CheckGoVet, code: 1, wantError: true},
		{check: CheckGoVet, code: 137, output: "killed", wantError: true},
	} {
		findings, err := commandFindings(tc.check, "app", tc.code, "", tc.output)
		if (err != nil) != tc.wantError {
			t.Fatalf("%s exit %d: findings=%v error=%v", tc.check, tc.code, findings, err)
		}
		if !tc.wantError && (len(findings) != 1 || findings[0].Message != tc.output || findings[0].Location.File != "app") {
			t.Fatalf("lost tool diagnostic: %v", findings)
		}
	}

	if findings, err := commandFindings(CheckGoVet, "app", 0, "", ""); err != nil || len(findings) != 0 {
		t.Fatalf("a clean run is not a finding: %v %v", findings, err)
	}
}

// go mod tidy -diff and go mod verify exit 1 for a mismatch and for a failure
// alike, so only their output can tell an untidy or tampered module from a
// proxy outage, and the outage must never pass.
func TestModFindingsSeparateMismatchesFromToolErrors(t *testing.T) {
	const diff = "diff current/go.sum tidy/go.sum\n--- current/go.sum\n+++ tidy/go.sum\n@@ -1 +0,0 @@\n-golang.org/x/text v0.3.0 h1:abc="
	for _, tc := range []struct {
		name      string
		step      modStep
		code      int
		stdout    string
		stderr    string
		wantError bool
	}{
		{name: "untidy manifests", step: modTidy, code: 1, stdout: diff},
		{name: "untidy with download noise", step: modTidy, code: 1, stdout: diff, stderr: "go: downloading golang.org/x/text v0.3.0"},
		{name: "checksum mismatch", step: modTidy, code: 1, stderr: "verifying golang.org/x/text@v0.3.0: checksum mismatch\n\tdownloaded: h1:abc=\n\tgo.sum:     h1:def=\n\nSECURITY ERROR\nThis download does NOT match an earlier download recorded in go.sum."},
		{name: "modified download", step: modVerify, code: 1, stderr: "golang.org/x/text v0.3.0: dir has been modified (/go/pkg/mod/golang.org/x/text@v0.3.0)"},
		{name: "modified zip", step: modVerify, code: 1, stderr: "golang.org/x/text v0.3.0: zip has been modified (/go/pkg/mod/cache/download/golang.org/x/text/@v/v0.3.0.zip)"},
		{name: "missing ziphash", step: modVerify, code: 1, stderr: "golang.org/x/text v0.3.0: missing ziphash: open hash file: no such file"},
		{name: "unreachable proxy on tidy", step: modTidy, code: 1, stderr: `go: example.com/app imports example.invalid/dep: unrecognized import path "example.invalid/dep": https fetch: Bad Gateway`, wantError: true},
		{name: "unreachable proxy on verify", step: modVerify, code: 1, stderr: `go: example.invalid/dep@v1.0.0: unrecognized import path "example.invalid/dep"`, wantError: true},
		{name: "verify diff output is not tidy's", step: modVerify, code: 1, stdout: diff, wantError: true},
		{name: "killed", step: modTidy, code: 137, stdout: diff, wantError: true},
		{name: "silent failure", step: modVerify, code: 1, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			findings, err := modFindings(tc.step, "app", tc.code, tc.stdout, tc.stderr)
			if (err != nil) != tc.wantError {
				t.Fatalf("findings=%v error=%v", findings, err)
			}
			if tc.wantError {
				return
			}

			want := strings.TrimSpace(tc.stdout + "\n" + tc.stderr)
			if len(findings) != 1 || findings[0].Code != string(CheckGoMod) || findings[0].Message != want || findings[0].Location.File != "app" {
				t.Fatalf("lost the go command's output: %+v", findings)
			}
		})
	}

	for _, step := range modSteps {
		if findings, err := modFindings(step, "app", 0, "all modules verified", ""); err != nil || len(findings) != 0 {
			t.Fatalf("%s: a clean run is not a finding: %v %v", step, findings, err)
		}
	}
}

// A location outside the source root, or one a tool already reported
// relatively, is left exactly as the tool wrote it.
func TestRepositoryPathOnlyRelativizesInsideTheSource(t *testing.T) {
	for _, tt := range []struct {
		root string
		file string
		want string
	}{
		{root: "/src", file: "/src/pkg/a.go", want: "pkg/a.go"},
		{root: "/src", file: "pkg/a.go", want: "pkg/a.go"},
		{root: "/src", file: "/elsewhere/a.go", want: "/elsewhere/a.go"},
		{root: "", file: "/src/a.go", want: "/src/a.go"},
	} {
		if got := repositoryPath(tt.root, tt.file); got != tt.want {
			t.Errorf("repositoryPath(%q, %q) = %q, want %q", tt.root, tt.file, got, tt.want)
		}
	}
}

func TestFindingsDetailsMatchTheDaggerEnvelope(t *testing.T) {
	details := findingsDetails([]finding{{Code: "SA5001", Message: "m", Location: location{File: "a.go", Line: 2, Column: 3}}})

	for _, want := range []string{`"findings"`, `"code":"SA5001"`, `"file":"a.go"`, `"line":2`} {
		if !strings.Contains(string(details), want) {
			t.Fatalf("details missing %s: %s", want, details)
		}
	}
}

func TestCoreURLsMatchTheSharedTable(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "runner", "testdata", "core-urls.json"))
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []struct {
			Code string `json:"code"`
			URL  string `json:"url"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}

	for _, test := range table.Cases {
		if got := coreURL(test.Code); got != test.URL {
			t.Errorf("coreURL(%q) = %q, want %q", test.Code, got, test.URL)
		}
	}
}
