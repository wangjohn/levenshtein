package main

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
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

// The steps that run the repository's own code, as root, must do it in a
// container whose Go caches no tool is ever built from: otherwise a test or a
// generator could rewrite, say, Staticcheck's source in the module cache and
// neuter every later linter build on a persistent engine. Each of these
// functions may take a binary built elsewhere, but the container it runs the
// repository in must come from untrustedGoContainer.
func TestRepositoryCodeRunsWithUntrustedCaches(t *testing.T) {
	trusted := []string{"goContainer", "linter", "gochecker", "apidiffer"}
	for file, function := range map[string]string{"checks.go": "goTest", "gocheck.go": "goGenerate", "mutation.go": "runGremlins"} {
		calls := calledFunctions(t, file, function)
		if !slices.Contains(calls, "untrustedGoContainer") {
			t.Errorf("%s in %s must run in untrustedGoContainer; it calls %v", function, file, calls)
		}
		for _, name := range trusted {
			if slices.Contains(calls, name) {
				t.Errorf("%s in %s runs repository code but calls %s, whose caches build the tools", function, file, name)
			}
		}
	}
}

// Tool builds and the steps that run repository code mount the same Go cache
// directories, but never one volume in common.
func TestToolAndUntrustedCachesAreDisjoint(t *testing.T) {
	tools := toolchain{Go: "1.27.1"}
	trusted := goCaches(tools, cacheTools)
	untrusted := goCaches(tools, cacheUntrusted)
	if len(trusted) == 0 || len(trusted) != len(untrusted) {
		t.Fatalf("tools %v, untrusted %v", trusted, untrusted)
	}

	for i, cache := range trusted {
		if cache.Path != untrusted[i].Path {
			t.Errorf("both kinds of step must cache the same directories: %v and %v", cache, untrusted[i])
		}
		for _, other := range untrusted {
			if cache.Volume == other.Volume {
				t.Errorf("%s is mounted both where tools are built and where repository code runs", cache.Volume)
			}
		}
		if !strings.Contains(cache.Volume, tools.Go) || !strings.Contains(untrusted[i].Volume, tools.Go) {
			t.Errorf("cache volumes must be keyed by the Go version: %v, %v", cache, untrusted[i])
		}
	}
}

// calledFunctions lists the plain function names one top-level function in
// file calls.
func calledFunctions(t *testing.T, file, function string) []string {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	var calls []string
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Name.Name != function {
			continue
		}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok {
				if name, ok := call.Fun.(*ast.Ident); ok {
					calls = append(calls, name.Name)
				}
			}
			return true
		})
		return calls
	}
	t.Fatalf("%s declares no function %s", file, function)
	return nil
}
