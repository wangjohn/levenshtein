package gocheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// testdata is the runner's fixture directory, shared with the Dagger
// self-test and the CLI's integration tests.
const testdata = "../../testdata"

// ruleTable is runner/testdata/import-rules.json. internal/verify validates
// levenshtein.json with a copy of ValidateImportRules against the same table.
type ruleTable struct {
	Fixture         ImportRules   `json:"fixture"`
	FixtureFindings []string      `json:"fixture_findings"`
	Valid           []ImportRules `json:"valid"`
	Invalid         []struct {
		Config ImportRules `json:"config"`
		Error  string      `json:"error"`
	} `json:"invalid"`
}

func loadRuleTable(t *testing.T) ruleTable {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testdata, "import-rules.json"))
	if err != nil {
		t.Fatal(err)
	}
	var table ruleTable
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Valid) == 0 || len(table.Invalid) == 0 || len(table.FixtureFindings) == 0 {
		t.Fatal("import-rules.json lost its cases")
	}
	return table
}

// testEnv runs the go command the way the executors do: the installed
// toolchain, and no workspace from outside the fixture.
func testEnv() []string {
	return withEnv(os.Environ(), "GOWORK=off", "GOTOOLCHAIN=local", "GOFLAGS=")
}

func TestValidateImportRulesAgreesWithTheSharedTable(t *testing.T) {
	table := loadRuleTable(t)
	for _, config := range append(table.Valid, table.Fixture) {
		if err := ValidateImportRules(config); err != nil {
			t.Errorf("valid rules %+v: %v", config, err)
		}
	}
	for _, tc := range table.Invalid {
		err := ValidateImportRules(tc.Config)
		if err == nil || !strings.Contains(err.Error(), tc.Error) {
			t.Errorf("rules %+v: want an error containing %q, got %v", tc.Config, tc.Error, err)
		}
	}
}

func TestImportsReportsEachViolatingImportAtItsPosition(t *testing.T) {
	table := loadRuleTable(t)
	good, err := Imports(t.Context(), filepath.Join(testdata, "imports-good"), "app", table.Fixture, testEnv())
	if err != nil || len(good.Findings) != 0 {
		t.Fatalf("imports-good must pass, generated file and excluded test included: %+v %v", good, err)
	}

	dir, err := filepath.Abs(filepath.Join(testdata, "imports-bad"))
	if err != nil {
		t.Fatal(err)
	}
	bad, err := Imports(t.Context(), dir, ".", table.Fixture, testEnv())
	if err != nil {
		t.Fatalf("imports-bad must fail for its imports, not error: %v", err)
	}
	var got []string
	for _, finding := range bad.Findings {
		if finding.Code != CodeImports || !strings.Contains(finding.Message, ": core stays") && !strings.Contains(finding.Message, ": store uses") {
			t.Errorf("finding lost its code or its rule's reason: %+v", finding)
		}
		_, quoted, _ := strings.Cut(finding.Message, `"`)
		imported, _, _ := strings.Cut(quoted, `"`)
		got = append(got, fmt.Sprintf("%s:%d %s", finding.Location.File, finding.Location.Line, imported))
	}
	if !slices.Equal(got, table.FixtureFindings) {
		t.Fatalf("imports-bad findings:\n got %v\nwant %v", got, table.FixtureFindings)
	}
}

func TestImportsRefusesAPackagePatternThatMatchesNothing(t *testing.T) {
	rules := ImportRules{Rules: []ImportRule{{Packages: []string{"./core", "./cores/..."}, Deny: []string{"net/http"}, Reason: "typo"}}}
	_, err := Imports(t.Context(), filepath.Join(testdata, "imports-good"), "app", rules, testEnv())
	if err == nil || !strings.Contains(err.Error(), `package pattern "./cores/..." matches no package in app`) {
		t.Fatalf("a misspelled package pattern must be an error: %v", err)
	}

	_, err = Imports(t.Context(), filepath.Join(testdata, "imports-good"), "app", ImportRules{}, testEnv())
	if err == nil || !strings.Contains(err.Error(), "nonempty rules") {
		t.Fatalf("an empty configuration must be an error: %v", err)
	}
}

func TestPatternsFollowTheGoCommand(t *testing.T) {
	for _, tc := range []struct {
		pattern string
		name    string
		want    bool
	}{
		{"./...", ".", true},
		{"./...", "./a/b", true},
		{"./core/...", "./core", true},
		{"./core/...", "./core/sub", true},
		{"./core/...", "./corex", false},
		{"./core", "./core/sub", false},
		{"./internal/.../testutil", "./internal/a/b/testutil", true},
		{"net/...", "net", true},
		{"net/...", "net/http", true},
		{"net/...", "network", false},
		{"dagger.io/...", "dagger.io/dagger", true},
		{"example.com/a.b", "example.com/aXb", false},
	} {
		if got := compilePattern(tc.pattern).match(tc.name); got != tc.want {
			t.Errorf("%q matches %q = %v, want %v", tc.pattern, tc.name, got, tc.want)
		}
	}
}

func TestStdMeansTheStandardLibrary(t *testing.T) {
	where := scope{local: "app/sub", module: "app"}
	for importPath, want := range map[string]bool{
		"fmt":                 true,
		"net/http":            true,
		"C":                   false,
		"app":                 false,
		"app/sub/x":           false,
		"example.com/x":       false,
		"golang.org/x/tools":  false,
		"internal/abi":        true,
		"dagger/levenshtein":  true,
		"vendor/golang.org/x": true,
	} {
		if got := where.matcher("std").match(where, importPath); got != want {
			t.Errorf("std matches %q = %v, want %v", importPath, got, want)
		}
	}
	if !where.matcher("./x/...").match(where, "app/sub/x/y") || !where.matcher(".").match(where, "app/sub") || where.matcher("./x").match(where, "./x") {
		t.Error(`"./" patterns must resolve against the checked directory's import path`)
	}
}

func TestDenyWinsOverAllow(t *testing.T) {
	where := scope{local: "app", module: "app"}
	rule := compiledRule{
		deny:  []importMatcher{where.matcher("os/exec")},
		allow: []importMatcher{where.matcher("std"), where.matcher("./core/...")},
	}
	for importPath, want := range map[string]string{
		"os":               "",
		"os/exec":          `denied by "os/exec"`,
		"app/core/x":       "",
		"app/api":          "matches nothing in the allow list",
		"example.com/lib":  "matches nothing in the allow list",
		"golang.org/x/mod": "matches nothing in the allow list",
	} {
		if got, _ := rule.violation(where, importPath); got != want {
			t.Errorf("%s: got %q, want %q", importPath, got, want)
		}
	}
}
