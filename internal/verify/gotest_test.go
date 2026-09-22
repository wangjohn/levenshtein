package verify

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testVerdict is what go-test must make of one recorded go test run.
type testVerdict string

const (
	verdictPass    testVerdict = "pass"
	verdictFinding testVerdict = "finding"
	verdictError   testVerdict = "error"
)

// testEventCase is one row of runner/testdata/test-events/cases.json.
type testEventCase struct {
	Name     string      `json:"name"`
	Exit     int         `json:"exit"`
	Want     testVerdict `json:"want"`
	Findings int         `json:"findings"`
	Contains string      `json:"contains"`
	Omits    string      `json:"omits"`
}

// go test exits 1 for a failing test and for a package that did not build
// alike, so only its events tell them apart. The table is real go test output,
// shared with the runner's copy of testFindings.
func TestTestFindingsSeparateFailuresFromBuildErrors(t *testing.T) {
	dir := filepath.Join("..", "..", "runner", "testdata", "test-events")
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
		t.Fatal("runner/testdata/test-events/cases.json has no cases")
	}

	for _, tc := range table.Cases {
		t.Run(tc.Name, func(t *testing.T) {
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
					t.Fatalf("want a pass: findings=%+v error=%v", findings, err)
				}
			case verdictError:
				if err == nil || !strings.Contains(err.Error(), tc.Contains) {
					t.Fatalf("want an error containing %q: findings=%+v error=%v", tc.Contains, findings, err)
				}
			case verdictFinding:
				if err != nil || len(findings) != tc.Findings {
					t.Fatalf("want %d findings: findings=%+v error=%v", tc.Findings, findings, err)
				}
				for _, finding := range findings {
					if finding.Code != string(CheckGoTest) || finding.Location.File != "app" || !strings.Contains(finding.Message, tc.Contains) {
						t.Fatalf("lost go test's output: %+v", finding)
					}
					if tc.Omits != "" && strings.Contains(finding.Message, tc.Omits) {
						t.Fatalf("kept output of a passing test %q: %s", tc.Omits, finding.Message)
					}
				}
			default:
				t.Fatalf("unknown want %q", tc.Want)
			}
		})
	}
}

// Anything but exit 1 with a failed package is an error, whatever the output.
func TestTestFindingsRefuseUnexpectedOutput(t *testing.T) {
	fail := `{"Action":"fail","Package":"example.com/app"}`
	for _, tc := range []struct {
		name   string
		code   int
		stdout string
		stderr string
	}{
		{name: "usage error", code: 2, stderr: "flag provided but not defined: -nope"},
		{name: "killed with failed packages", code: 137, stdout: fail},
		{name: "not events", code: 1, stdout: "FAIL\texample.com/app\t0.01s\n"},
		{name: "not events on success", code: 0, stdout: "ok\texample.com/app\t0.01s\n"},
		{name: "silent failure", code: 1},
		{name: "no cgo", code: 2, stderr: "go: -race requires cgo; enable cgo by setting CGO_ENABLED=1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if findings, err := testFindings("app", tc.code, tc.stdout, tc.stderr); err == nil {
				t.Fatalf("want an error, got findings %+v", findings)
			}
		})
	}
}

// The report shows go test's text, not its event stream.
func TestTestTranscriptIsGoTestText(t *testing.T) {
	stdout, err := os.ReadFile(filepath.Join("..", "..", "runner", "testdata", "test-events", "build.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	events, err := testEvents(string(stdout))
	if err != nil {
		t.Fatal(err)
	}

	want := "# example.com/test-build [example.com/test-build.test]\n./sum_test.go:6:21: cannot use Add(1, 2) (value of type int) as string value in variable declaration\nFAIL\texample.com/test-build [build failed]\n"
	if got := testTranscript(events); got != want {
		t.Fatalf("transcript:\n%s\nwant:\n%s", got, want)
	}
}

// A fresh run bypasses go test's own result cache; any other run may use it,
// since Levenshtein's cache already decided the result must be recomputed.
func TestTestArgsBypassGoTestCacheOnlyWhenFresh(t *testing.T) {
	cached, fresh := testArgs(false), testArgs(true)
	for _, args := range [][]string{cached, fresh} {
		for _, flag := range []string{"-race", "-json", "-vet=off", "-timeout=10m"} {
			if !slices.Contains(args, flag) {
				t.Fatalf("%v lacks %s", args, flag)
			}
		}
		if args[len(args)-1] != "./..." {
			t.Fatalf("%v does not test every package", args)
		}
	}
	if slices.Contains(cached, "-count=1") || !slices.Contains(fresh, "-count=1") {
		t.Fatalf("cached %v, fresh %v", cached, fresh)
	}
}
