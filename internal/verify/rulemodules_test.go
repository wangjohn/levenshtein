package verify

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

// ruleModulesConfig is a configuration with one rule module, a go-lint check
// that runs it, one that opts out, and a go-http check, which never runs
// community rules. modules and checks replace the defaults when set.
func ruleModulesConfig(t *testing.T, modules, lint string) Config {
	t.Helper()
	if modules == "" {
		modules = `{"github.com/acme/lvrules-errors": {"version": "v1.4.0", "namespace": "errs", "select": ["errs_*", "-errs_wrapf"], "advisory": ["errs_sentinel"], "settings": {"errs_nopanic": {"allow": "Must,Should"}}}}`
	}
	if lint == "" {
		lint = `{"checks": ["gocognit", "errs_wrapf"]}`
	}
	cfg, err := Parse([]byte(`{"version": 1,
		"targets": {"app": {"dir": ".", "inputs": ["."]}},
		"environments": {"go": {"executor": "dagger"}, "host": {"executor": "native"}},
		"rule_modules": ` + modules + `,
		"checks": {
			"lint": {"kind": "go-lint", "target": "app", "environment": "go", "lint": ` + lint + `},
			"plain": {"kind": "go-lint", "target": "app", "environment": "host", "lint": {"rule_modules": false}},
			"http": {"kind": "go-http", "target": "app", "environment": "go"}
		},
		"runs": {"branch": {"checks": ["lint", "plain", "http"]}}}`))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestRuleModulesArePlannedForGoLintChecksOnly(t *testing.T) {
	cfg := ruleModulesConfig(t, "", "")

	plan, err := cfg.Plan(t.TempDir(), "branch")
	if err != nil {
		t.Fatal(err)
	}

	want := []PlannedRuleModule{{
		Path:      "github.com/acme/lvrules-errors",
		Version:   "v1.4.0",
		Namespace: "errs",
		Select:    []string{"errs_*", "-errs_wrapf"},
		Advisory:  []string{"errs_sentinel"},
		Settings:  map[string]map[string]string{"errs_nopanic": {"allow": "Must,Should"}},
	}}
	lint := plan.Checks[0]
	encoded, _ := json.Marshal(lint.RuleModules)
	wanted, _ := json.Marshal(want)
	if string(encoded) != string(wanted) {
		t.Errorf("lint plans %s, want %s", encoded, wanted)
	}
	if !slices.Equal(lint.Check.coreLintChecks(), []string{"gocognit"}) {
		t.Errorf("the core linter must not see community patterns: %v", lint.Check.coreLintChecks())
	}
	for _, check := range plan.Checks[1:] {
		if check.RuleModules != nil {
			t.Errorf("%s must run no rule modules: %+v", check.ID, check.RuleModules)
		}
	}
}

// A result key covers the pins, so moving a pin reruns the check, and a check
// without rule modules keeps the key it had before rule modules existed.
func TestRuleModulesArePartOfTheResultKey(t *testing.T) {
	cfg := ruleModulesConfig(t, "", "")
	plan, err := cfg.Plan(t.TempDir(), "branch")
	if err != nil {
		t.Fatal(err)
	}
	lint, plain := plan.Checks[0], plan.Checks[1]

	moved := lint
	moved.RuleModules = slices.Clone(lint.RuleModules)
	moved.RuleModules[0].Version = "v1.5.0"
	if digest(moved) == digest(lint) {
		t.Error("changing a rule module's version must change the result key")
	}
	encoded, err := json.Marshal(plain)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "rule_modules\":[") {
		t.Errorf("a check without rule modules must not change its key: %s", encoded)
	}
}

func TestRuleModulesRejectWhatCannotBePinnedOrSelected(t *testing.T) {
	const acme = "github.com/acme/lvrules-errors"
	module := func(fields string) string { return `{"` + acme + `": {` + fields + `}}` }
	for _, test := range []struct {
		name    string
		modules string
		lint    string
		message string
	}{
		{"branch", module(`"version": "main", "namespace": "errs", "select": ["errs_*"]`), "", "must be an exact tag or pseudo-version"},
		{"latest", module(`"version": "latest", "namespace": "errs", "select": ["errs_*"]`), "", "must be an exact tag or pseudo-version"},
		{"short version", module(`"version": "v1.4", "namespace": "errs", "select": ["errs_*"]`), "", "must be an exact tag or pseudo-version"},
		{"uppercase namespace", module(`"version": "v1.4.0", "namespace": "Errs", "select": ["errs_*"]`), "", "lowercase letters only"},
		{"reserved namespace", module(`"version": "v1.4.0", "namespace": "lv", "select": ["lv_*"]`), "", `namespace "lv" is reserved`},
		{"example namespace", module(`"version": "v1.4.0", "namespace": "example", "select": ["example_*"]`), "", `namespace "example" is reserved`},
		{"empty select", module(`"version": "v1.4.0", "namespace": "errs", "select": []`), "", "select must name at least one rule"},
		{"select in another namespace", module(`"version": "v1.4.0", "namespace": "errs", "select": ["sql_*"]`), "", "must be a pattern in this module's namespace"},
		{"core pattern in select", module(`"version": "v1.4.0", "namespace": "errs", "select": ["SA4006"]`), "", "must be a pattern in this module's namespace"},
		{"setting for a glob", module(`"version": "v1.4.0", "namespace": "errs", "select": ["errs_*"], "settings": {"errs_*": {"allow": "x"}}`), "", "must be one rule's code"},
		{"settings differing in case", module(`"version": "v1.4.0", "namespace": "errs", "select": ["errs_*"], "settings": {"errs_nopanic": {"allow": "a"}, "errs_NoPanic": {"allow": "b"}}`), "", "name the same rule"},
		{"advisory nothing selects", module(`"version": "v1.4.0", "namespace": "errs", "select": ["errs_nopanic"], "advisory": ["errs_sentinel"]`), "", `advisory rule "errs_sentinel" is not selected by any go-lint check`},
		{"undeclared namespace in a check", "", `{"checks": ["other_*"]}`, `names namespace "other"`},
		{"malformed community pattern", "", `{"checks": ["errs_no-panic"]}`, "must be one community pattern"},
		{"community pattern with rule modules off", "", `{"checks": ["errs_*"], "rule_modules": false}`, "sets rule_modules to false"},
		{"empty lint options", "", `{}`, "need a nonempty checks list or rule_modules"},
		{"invalid module path", `{"not a path": {"version": "v1.0.0", "namespace": "errs", "select": ["errs_*"]}}`, "", `rule_modules "not a path"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := ruleModulesConfig(t, test.modules, test.lint)

			_, err := cfg.Plan(t.TempDir(), "branch")

			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Errorf("got %v, want an error containing %q", err, test.message)
			}
		})
	}
}

func TestTwoModulesCannotShareANamespace(t *testing.T) {
	cfg := ruleModulesConfig(t, `{
		"github.com/acme/lvrules-errors": {"version": "v1.4.0", "namespace": "errs", "select": ["errs_*"]},
		"github.com/other/lvrules-errs": {"version": "v0.2.0", "namespace": "errs", "select": ["errs_*"]}}`, `{"checks": ["gocognit"]}`)

	_, err := cfg.Plan(t.TempDir(), "branch")

	if err == nil || !strings.Contains(err.Error(), `both declare namespace "errs"`) {
		t.Errorf("got %v", err)
	}
}

// The example module in this repository may use its reserved namespace, and
// pseudo-versions and /v2 paths are exact pins.
func TestRuleModulesAcceptEveryExactPin(t *testing.T) {
	cfg := ruleModulesConfig(t, `{
		"github.com/wangjohn/levenshtein/examples/rule-module": {"version": "v0.0.0-20260901000000-abcdefabcdef", "namespace": "example", "select": ["example_*"]},
		"github.com/acme/lvrules-errors/v2": {"version": "v2.0.1", "namespace": "errs", "select": ["errs_*"]}}`, `{"checks": ["-errs_wrapf"]}`)

	if _, err := cfg.Plan(t.TempDir(), "branch"); err != nil {
		t.Fatal(err)
	}
}

// patternCases is runner/testdata/community-patterns.json, which
// runner/community's original of the pattern syntax and selection loads too.
type patternCases struct {
	Syntax []struct {
		Pattern string `json:"pattern"`
		Valid   bool   `json:"valid"`
	} `json:"syntax"`
	Selection []struct {
		Name     string   `json:"name"`
		Patterns []string `json:"patterns"`
		Code     string   `json:"code"`
		Want     bool     `json:"want"`
	} `json:"selection"`
}

func TestCommunityPatternsMatchTheSharedTable(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "runner", "testdata", "community-patterns.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases patternCases
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}

	for _, test := range cases.Syntax {
		if got := communityPattern.MatchString(test.Pattern); got != test.Valid {
			t.Errorf("communityPattern.MatchString(%q) = %v, want %v", test.Pattern, got, test.Valid)
		}
	}
	for _, test := range cases.Selection {
		if got := communityAllowed(test.Patterns, test.Code); got != test.Want {
			t.Errorf("%s: communityAllowed(%v, %q) = %v, want %v", test.Name, test.Patterns, test.Code, got, test.Want)
		}
	}
}

func writeRuleModuleNotices(t *testing.T, notices string) string {
	t.Helper()
	shared := t.TempDir()
	if err := os.MkdirAll(filepath.Join(shared, "runner"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(shared, "runner", "rule-modules.json"), []byte(notices), 0o600); err != nil {
		t.Fatal(err)
	}
	return shared
}

func TestAWithdrawnVersionIsAConfigurationError(t *testing.T) {
	plan, err := ruleModulesConfig(t, "", "").Plan(t.TempDir(), "branch")
	if err != nil {
		t.Fatal(err)
	}
	withdrawn := writeRuleModuleNotices(t, `{"comment": "", "modules": [
		{"path": "github.com/acme/lvrules-errors", "version": "v1.4.0", "status": "withdrawn", "reason": "uploads source to a third party", "replacement": "github.com/acme/lvrules-errors@v1.4.1"}]}`)
	deprecated := writeRuleModuleNotices(t, `{"comment": "", "modules": [
		{"path": "github.com/acme/lvrules-errors", "version": "v1.4.0", "status": "deprecated", "reason": "superseded"}]}`)
	unknown := writeRuleModuleNotices(t, `{"comment": "", "modules": [
		{"path": "github.com/acme/lvrules-errors", "version": "v1.4.0", "status": "retired", "reason": "?"}]}`)

	err = CheckWithdrawnRuleModules(plan, withdrawn)
	want := `rule_modules "github.com/acme/lvrules-errors": v1.4.0 is withdrawn: uploads source to a third party; use github.com/acme/lvrules-errors@v1.4.1 instead`
	if err == nil || err.Error() != want {
		t.Errorf("got %v\nwant %s", err, want)
	}
	if err := CheckWithdrawnRuleModules(plan, deprecated); err != nil {
		t.Errorf("a deprecated version still runs: %v", err)
	}
	if err := CheckWithdrawnRuleModules(plan, t.TempDir()); err != nil {
		t.Errorf("a shared checkout without the list withdraws nothing: %v", err)
	}
	if err := CheckWithdrawnRuleModules(plan, unknown); err == nil || !strings.Contains(err.Error(), `unknown status "retired"`) {
		t.Errorf("an unknown status must be refused: %v", err)
	}
}

func TestTheShippedRuleModuleListIsValid(t *testing.T) {
	if _, err := ruleModuleNotices(filepath.Join("..", "..")); err != nil {
		t.Fatal(err)
	}
}

func TestANativeGoLintCheckSaysItSkippedCommunityRules(t *testing.T) {
	plan, err := ruleModulesConfig(t, "", "").Plan(t.TempDir(), "branch")
	if err != nil {
		t.Fatal(err)
	}
	native, dagger := plan.Checks[0], plan.Checks[0]
	native.Environment.Executor = ExecutorNative
	dagger.Environment.Executor = ExecutorDagger

	if warnings := skippedRuleModules(Request{PlannedCheck: plan.Checks[1]}); warnings != nil {
		t.Errorf("a check that opted out skipped nothing: %+v", warnings)
	}
	if warnings := skippedRuleModules(Request{PlannedCheck: dagger}); warnings != nil {
		t.Errorf("the Dagger executor runs community rules: %+v", warnings)
	}
	warnings := skippedRuleModules(Request{PlannedCheck: native})
	if len(warnings) != 1 || warnings[0].Kind != WarningRuleModulesSkipped {
		t.Errorf("native warnings = %+v", warnings)
	}
}

func TestLintReportsCarryAdvisoryFindingsAndWarnings(t *testing.T) {
	passed := withLintReport(Result{Status: StatusPassed}, `{"findings": [{"code": "errs_sentinel", "message": "m", "location": {"file": "a.go", "line": 1, "column": 1}, "advisory": true}], "warnings": [{"kind": "rule-renamed", "message": "old name"}]}`)
	quiet := withLintReport(Result{Status: StatusPassed}, `{"findings": [], "warnings": []}`)
	broken := withLintReport(Result{Status: StatusPassed}, `not json`)

	if passed.Status != StatusPassed || !strings.Contains(string(passed.Details), `"advisory":true`) || len(passed.Warnings) != 1 || passed.Warnings[0].Kind != WarningRuleRenamed {
		t.Errorf("an advisory finding must leave the check passing with its details: %+v", passed)
	}
	if quiet.Details != nil || quiet.Warnings != nil {
		t.Errorf("an empty report adds nothing: %+v", quiet)
	}
	if broken.Status != StatusError {
		t.Errorf("an unreadable report must be an error: %+v", broken)
	}
}

func TestACheckErrorKeepsTheFindingsItHas(t *testing.T) {
	err := &gqlerror.Error{Message: "Go policy lint failed", Extensions: map[string]any{
		"levenshteinFindings": []map[string]any{{"code": "SA4006", "message": "m", "location": map[string]any{"file": "a.go", "line": 3, "column": 1}}},
		"levenshteinError":    "errs_nopanic (github.com/acme/lvrules-errors@v1.4.0) failed on 3 packages: panic: boom",
		"levenshteinWarnings": []map[string]any{{"kind": "rule-deprecated", "message": "errs_wrapf is deprecated"}},
	}}

	result := daggerResult(err)

	if result.Status != StatusError || !strings.HasPrefix(result.Error, "errs_nopanic (") {
		t.Errorf("status %s, error %q", result.Status, result.Error)
	}
	if !strings.Contains(string(result.Details), `"SA4006"`) {
		t.Errorf("core findings must survive a community failure: %s", result.Details)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Kind != WarningRuleDeprecated {
		t.Errorf("warnings = %+v", result.Warnings)
	}
	if daggerResult(errors.New("engine unavailable")).Warnings != nil {
		t.Error("a plain error carries no warnings")
	}
}
