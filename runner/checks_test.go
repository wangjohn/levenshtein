package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCommandFailuresDoNotBecomePassingResults(t *testing.T) {
	for _, tc := range []struct {
		check     checkName
		code      int
		output    string
		wantError bool
	}{
		{checkVet, 1, "copylocks: copies lock value", false},
		{checkWorkflow, 1, "unknown job", false},
		{checkVuln, 3, "reachable vulnerability", false},
		{checkVuln, 1, "database unavailable", true},
		{checkWorkflow, 3, "configuration error", true},
		{checkVet, 1, "", true},
		{checkVet, 137, "killed", true},
	} {
		findings, err := commandFindings(tc.check, "app", tc.code, "", tc.output)
		if (err != nil) != tc.wantError {
			t.Fatalf("%s exit %d: findings=%v error=%v", tc.check, tc.code, findings, err)
		}
		if !tc.wantError && (len(findings) != 1 || findings[0].Message != tc.output) {
			t.Fatalf("lost tool diagnostic: %v", findings)
		}
	}
}

// This mirrors internal/verify's go-mod test: both commands exit 1 for a
// mismatch and for a failure alike, and only a mismatch is a finding.
func TestModFindingsSeparateMismatchesFromToolErrors(t *testing.T) {
	for _, tc := range []struct {
		step      modStep
		code      int
		stdout    string
		stderr    string
		wantError bool
	}{
		{modTidy, 1, "diff current/go.mod tidy/go.mod\n-require example.com/unused v0.0.0", "", false},
		{modTidy, 1, "", "verifying example.com/dep@v1.0.0: checksum mismatch\n\nSECURITY ERROR", false},
		{modVerify, 1, "", "example.com/dep v1.0.0: dir has been modified (/go/pkg/mod/example.com/dep@v1.0.0)", false},
		{modTidy, 1, "", `go: example.com/app imports example.invalid/dep: unrecognized import path "example.invalid/dep"`, true},
		{modVerify, 1, "", `go: example.invalid/dep@v1.0.0: unrecognized import path "example.invalid/dep"`, true},
		{modTidy, 137, "diff current/go.mod tidy/go.mod", "", true},
		{modVerify, 1, "", "", true},
	} {
		findings, err := modFindings(tc.step, "app", tc.code, tc.stdout, tc.stderr)
		if (err != nil) != tc.wantError {
			t.Fatalf("%s exit %d: findings=%v error=%v", tc.step, tc.code, findings, err)
		}
		if !tc.wantError && (len(findings) != 1 || findings[0].Code != string(checkMod) || findings[0].Message != strings.TrimSpace(tc.stdout+"\n"+tc.stderr)) {
			t.Fatalf("lost the go command's output: %v", findings)
		}
	}
	if findings, err := modFindings(modVerify, "app", 0, "all modules verified", ""); err != nil || len(findings) != 0 {
		t.Fatalf("a clean run is not a finding: %v %v", findings, err)
	}
}

// A direct Dagger call without a nonce could be answered with a stale verdict,
// so the checks whose state no source input covers refuse it before running.
func TestFreshChecksRequireANonce(t *testing.T) {
	for _, check := range []checkName{checkVuln, checkMod} {
		err := (&Levenshtein{}).SharedCheck(t.Context(), nil, string(check), ".", "")
		if err == nil || !strings.Contains(err.Error(), string(check)+" requires a unique nonce") {
			t.Fatalf("%s ran without a nonce: %v", check, err)
		}
	}
}

// testVerdict is what go-test must make of one recorded go test run.
type testVerdict string

const (
	verdictPass    testVerdict = "pass"
	verdictFinding testVerdict = "finding"
	verdictError   testVerdict = "error"
)

// testEventCase is one row of testdata/test-events/cases.json.
type testEventCase struct {
	Name     string      `json:"name"`
	Exit     int         `json:"exit"`
	Want     testVerdict `json:"want"`
	Findings int         `json:"findings"`
	Contains string      `json:"contains"`
	Omits    string      `json:"omits"`
}

// This reads the same table of real go test output as internal/verify's
// go-test test, so the two copies of testFindings cannot drift apart: a
// failing test or a data race is a finding, a package that did not build is an
// error.
func TestTestFindingsSeparateFailuresFromBuildErrors(t *testing.T) {
	dir := filepath.Join("testdata", "test-events")
	data, err := os.ReadFile(filepath.Join(dir, "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Cases []testEventCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Cases) == 0 {
		t.Fatal("testdata/test-events/cases.json has no cases")
	}

	for _, tc := range table.Cases {
		stdout, err := os.ReadFile(filepath.Join(dir, tc.Name+".jsonl"))
		if err != nil {
			t.Fatal(err)
		}
		stderr, err := os.ReadFile(filepath.Join(dir, tc.Name+".stderr"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}

		findings, err := testFindings("app", tc.Exit, string(stdout), string(stderr))
		switch tc.Want {
		case verdictPass:
			if err != nil || len(findings) != 0 {
				t.Fatalf("%s: want a pass: findings=%v error=%v", tc.Name, findings, err)
			}
		case verdictError:
			if err == nil || !strings.Contains(err.Error(), tc.Contains) {
				t.Fatalf("%s: want an error containing %q: findings=%v error=%v", tc.Name, tc.Contains, findings, err)
			}
		case verdictFinding:
			if err != nil || len(findings) != tc.Findings {
				t.Fatalf("%s: want %d findings: findings=%v error=%v", tc.Name, tc.Findings, findings, err)
			}
			for _, finding := range findings {
				if finding.Code != string(checkTest) || finding.Location.File != "app" || !strings.Contains(finding.Message, tc.Contains) {
					t.Fatalf("%s: lost go test's output: %v", tc.Name, finding)
				}
				if tc.Omits != "" && strings.Contains(finding.Message, tc.Omits) {
					t.Fatalf("%s: kept output of a passing test %q: %s", tc.Name, tc.Omits, finding.Message)
				}
			}
		default:
			t.Fatalf("%s: unknown want %q", tc.Name, tc.Want)
		}
	}

	for _, tc := range []struct {
		code   int
		stdout string
		stderr string
	}{
		{2, "", "flag provided but not defined: -nope"},
		{137, `{"Action":"fail","Package":"example.com/app"}`, ""},
		{1, "FAIL\texample.com/app\t0.01s\n", ""},
		{1, "", ""},
	} {
		if findings, err := testFindings("app", tc.code, tc.stdout, tc.stderr); err == nil {
			t.Fatalf("exit %d must be an error: %v", tc.code, findings)
		}
	}
	if cached, fresh := testArgs(false), testArgs(true); slices.Contains(cached, "-count=1") || !slices.Contains(fresh, "-count=1") || !slices.Contains(cached, "-race") {
		t.Fatalf("cached %v, fresh %v", cached, fresh)
	}
}

func TestStaticcheckWildcardDoesNotAcceptCompilerErrors(t *testing.T) {
	for _, code := range []string{"SA4006", "compile"} {
		output := `{"code":"` + code + `","message":"finding","location":{"file":"/src/a.go","line":1}}`
		_, err := parseFindings(1, output, "", []string{"SA*"})
		if (err == nil) != (code == "SA4006") {
			t.Fatalf("unexpected result for %s: %v", code, err)
		}
	}
}
