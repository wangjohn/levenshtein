package community

import (
	"flag"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/tools/go/analysis"
)

const errsPath = "example.com/lvrules-errors"

func testAnalyzer(name, doc string) *analysis.Analyzer {
	analyzer := &analysis.Analyzer{
		Name: name,
		Doc:  doc,
		URL:  "https://example.com/rules/" + name,
		Run:  func(*analysis.Pass) (any, error) { return nil, nil },
	}
	analyzer.Flags.String("allow", "", "names that may panic")
	return analyzer
}

// errsModule is a module with three rules, one deprecated, and one rename.
func errsModule() Module {
	return Module{
		Path:      errsPath,
		Version:   "v1.4.0",
		Namespace: "errs",
		Renamed:   map[string]string{"panics": "nopanic"},
		Analyzers: []*analysis.Analyzer{
			testAnalyzer("nopanic", "report panics in library code"),
			testAnalyzer("sentinel", "compare errors with errors.Is"),
			testAnalyzer("wrapf", "wrap errors with %w\n\nDeprecated: use errorlint instead."),
		},
	}
}

func errsConfig(selected ...string) ModuleConfig {
	return ModuleConfig{Path: errsPath, Version: "v1.4.0", Namespace: "errs", Select: selected}
}

func selectedCodes(p plan) []string {
	var codes []string
	for _, rule := range p.Selected {
		codes = append(codes, rule.Code)
	}
	return codes
}

func TestResolveSelectsWithTheLastMatchingPattern(t *testing.T) {
	for _, test := range []struct {
		name   string
		module ModuleConfig
		checks []string
		want   []string
	}{
		{"namespace", errsConfig("errs_*"), nil, []string{"errs_nopanic", "errs_sentinel", "errs_wrapf"}},
		{"literal prefix", errsConfig("errs_no*", "errs_s*"), nil, []string{"errs_nopanic", "errs_sentinel"}},
		{"module turns one off", errsConfig("errs_*", "-errs_wrapf"), nil, []string{"errs_nopanic", "errs_sentinel"}},
		{"check turns one back on", errsConfig("errs_*", "-errs_wrapf"), []string{"errs_wrapf"}, []string{"errs_nopanic", "errs_sentinel", "errs_wrapf"}},
		{"check turns all off", errsConfig("errs_*"), []string{"-errs_*"}, nil},
		{"case is ignored", errsConfig("ERRS_NoPanic"), nil, []string{"errs_nopanic"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			resolved, err := resolve([]Module{errsModule()}, Config{Modules: []ModuleConfig{test.module}, Checks: test.checks})
			if err != nil {
				t.Fatal(err)
			}

			if got := selectedCodes(resolved); !slices.Equal(got, test.want) {
				t.Errorf("selected %v, want %v", got, test.want)
			}
		})
	}
}

func TestResolveReportsWhatFindingsCarry(t *testing.T) {
	cfg := errsConfig("errs_nopanic", "errs_sentinel")
	cfg.Advisory = []string{"errs_sentinel"}

	resolved, err := resolve([]Module{errsModule()}, Config{Modules: []ModuleConfig{cfg}})
	if err != nil {
		t.Fatal(err)
	}

	want := []RuleReport{
		{Code: "errs_nopanic", Source: errsPath + "@v1.4.0", URL: "https://example.com/rules/nopanic"},
		{Code: "errs_sentinel", Source: errsPath + "@v1.4.0", URL: "https://example.com/rules/sentinel", Advisory: true},
		{Code: CodeMixed, URL: DirectivesURL},
		{Code: CodeRenamed, URL: DirectivesURL},
	}
	if !slices.Equal(resolved.Report.Rules, want) {
		t.Errorf("rules = %+v\nwant %+v", resolved.Report.Rules, want)
	}
	if !resolved.advisory("errs_sentinel") || resolved.advisory("errs_nopanic") || resolved.advisory("errs_*") || resolved.advisory("SA4006") {
		t.Errorf("advisory codes are wrong: %v", resolved.Advisory)
	}
}

func TestResolveWarnsAboutOldNamesAndDeprecatedRules(t *testing.T) {
	cfg := errsConfig("errs_panics", "errs_wrapf")
	cfg.Settings = map[string]map[string]string{"errs_panics": {"allow": "Must"}}

	resolved, err := resolve([]Module{errsModule()}, Config{Modules: []ModuleConfig{cfg}})
	if err != nil {
		t.Fatal(err)
	}

	if got := selectedCodes(resolved); !slices.Equal(got, []string{"errs_nopanic", "errs_wrapf"}) {
		t.Errorf("an old name must select the renamed rule: %v", got)
	}
	var kinds []WarningKind
	for _, warning := range resolved.Report.Warnings {
		kinds = append(kinds, warning.Kind)
	}
	want := []WarningKind{WarningRuleRenamed, WarningRuleRenamed, WarningRuleDeprecated}
	if !slices.Equal(kinds, want) {
		t.Errorf("warnings = %+v, want kinds %v", resolved.Report.Warnings, want)
	}
	if !strings.Contains(resolved.Report.Warnings[2].Message, "use errorlint instead") {
		t.Errorf("the deprecation warning must quote the notice: %q", resolved.Report.Warnings[2].Message)
	}
	if len(resolved.Settings) != 1 || resolved.Settings[0].Rule.Code != "errs_nopanic" {
		t.Errorf("a setting under the old name must reach the renamed rule: %+v", resolved.Settings)
	}
}

func TestResolveRefusesWhatWouldLeaveARuleSilentlyOff(t *testing.T) {
	withSettings := errsConfig("errs_*")
	withSettings.Settings = map[string]map[string]string{"errs_nopanic": {"alow": "Must"}}
	withUnknownRule := errsConfig("errs_*")
	withUnknownRule.Settings = map[string]map[string]string{"errs_nopanc": {"allow": "Must"}}
	withAdvisory := errsConfig("errs_*")
	withAdvisory.Advisory = []string{"errs_sentinal"}
	foreignAdvisory := errsConfig("errs_*")
	foreignAdvisory.Advisory = []string{"other_x"}
	wrongVersion := errsConfig("errs_*")
	wrongVersion.Version = "v1.5.0"
	twice := errsConfig("errs_*")
	twice.Settings = map[string]map[string]string{"errs_nopanic": {"allow": "Must"}, "errs_panics": {"allow": "Should"}}
	wrongNamespace := errsConfig("errs_*")
	wrongNamespace.Namespace = "errors"

	for _, test := range []struct {
		name    string
		cfg     Config
		message string
	}{
		{"misspelled rule", Config{Modules: []ModuleConfig{errsConfig("errs_nopanc")}}, `pattern "errs_nopanc" matches no rule`},
		{"undeclared namespace", Config{Modules: []ModuleConfig{errsConfig("errs_*")}, Checks: []string{"other_*"}}, `names namespace "other"`},
		{"core pattern", Config{Modules: []ModuleConfig{errsConfig("SA4006")}}, "not a community pattern"},
		{"empty select", Config{Modules: []ModuleConfig{errsConfig()}}, "select must name at least one rule"},
		{"unknown setting", Config{Modules: []ModuleConfig{withSettings}}, `has no setting "alow"; its settings are allow`},
		{"old and new name both set", Config{Modules: []ModuleConfig{twice}}, `settings "errs_nopanic" and "errs_panics" both configure errs_nopanic`},
		{"setting for unknown rule", Config{Modules: []ModuleConfig{withUnknownRule}}, `settings name "errs_nopanc"`},
		{"misspelled advisory", Config{Modules: []ModuleConfig{withAdvisory}}, `advisory: pattern "errs_sentinal" matches no rule`},
		{"advisory in another namespace", Config{Modules: []ModuleConfig{foreignAdvisory}}, "must start with the module's namespace"},
		{"version differs", Config{Modules: []ModuleConfig{wrongVersion}}, "this linter was built with " + errsPath + "@v1.4.0"},
		{"namespace differs", Config{Modules: []ModuleConfig{wrongNamespace}}, `declares namespace "errors"`},
		{"module not configured", Config{}, "compiled in but not configured"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolve([]Module{errsModule()}, test.cfg)

			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Errorf("got %v, want an error containing %q", err, test.message)
			}
		})
	}
}

func TestModulesMustKeepTheContract(t *testing.T) {
	shared := testAnalyzer("shared", "one analyzer in two modules")
	noURL := testAnalyzer("nourl", "a rule without a page")
	noURL.URL = ""
	oneLine := testAnalyzer("valid", "valid")

	for _, test := range []struct {
		name    string
		modules []Module
		message string
	}{
		{"reserved namespace", []Module{{Path: "example.com/m", Version: "v1.0.0", Namespace: "lv", Analyzers: []*analysis.Analyzer{oneLine}}}, `namespace "lv" is reserved`},
		{"example namespace outside the example module", []Module{{Path: "example.com/m", Version: "v1.0.0", Namespace: "example", Analyzers: []*analysis.Analyzer{oneLine}}}, `namespace "example" is reserved`},
		{"digits in the namespace", []Module{{Path: "example.com/m", Version: "v1.0.0", Namespace: "sa1", Analyzers: []*analysis.Analyzer{oneLine}}}, "lowercase letters only"},
		{"no analyzers", []Module{{Path: "example.com/m", Version: "v1.0.0", Namespace: "m"}}, "returns no analyzers"},
		{"missing URL", []Module{{Path: "example.com/m", Version: "v1.0.0", Namespace: "m", Analyzers: []*analysis.Analyzer{noURL}}}, "absolute http(s) page"},
		{"names differ only in case", []Module{{Path: "example.com/m", Version: "v1.0.0", Namespace: "m", Analyzers: []*analysis.Analyzer{oneLine, testAnalyzer("VALID", "valid")}}}, "ignoring case"},
		{"rename to nothing", []Module{{Path: "example.com/m", Version: "v1.0.0", Namespace: "m", Renamed: map[string]string{"old": "gone"}, Analyzers: []*analysis.Analyzer{oneLine}}}, "which is not a rule"},
		{"shared analyzer", []Module{
			{Path: "example.com/a", Version: "v1.0.0", Namespace: "a", Analyzers: []*analysis.Analyzer{shared}},
			{Path: "example.com/b", Version: "v1.0.0", Namespace: "b", Analyzers: []*analysis.Analyzer{shared}},
		}, "would share its settings"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := resolve(test.modules, Config{})

			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Errorf("got %v, want an error containing %q", err, test.message)
			}
		})
	}
}

func TestExampleModuleMayUseTheExampleNamespace(t *testing.T) {
	if err := ValidNamespace(ExampleModule, "example"); err != nil {
		t.Error(err)
	}
}

func TestDocsGainATitleOnlyWhenMissing(t *testing.T) {
	for doc, want := range map[string]string{
		"one line":                    "one line\n\n",
		"summary\nmore detail":        "summary\n\nmore detail",
		"summary\n\nalready a title":  "summary\n\nalready a title",
		"summary\n\nDeprecated: gone": "summary\n\nDeprecated: gone",
	} {
		if got := titled(doc); got != want {
			t.Errorf("titled(%q) = %q, want %q", doc, got, want)
		}
	}
}

// The resolver must not lose a flag's value between validation and use.
func TestSettingsApplyThroughTheAnalyzerFlags(t *testing.T) {
	module := errsModule()
	cfg := errsConfig("errs_nopanic")
	cfg.Settings = map[string]map[string]string{"errs_nopanic": {"allow": "Must,Should"}}
	flags := flag.NewFlagSet("", flag.ContinueOnError)
	configPath := t.TempDir() + "/config.json"
	writeJSON(t, configPath, Config{Modules: []ModuleConfig{cfg}})

	_, err := load([]Module{module}, flags, configPath, t.TempDir()+"/report.json")
	if err != nil {
		t.Fatal(err)
	}

	if got := module.Analyzers[0].Flags.Lookup("allow").Value.String(); got != "Must,Should" {
		t.Errorf("allow = %q", got)
	}
}

// A rule that requires another selected rule must not make the guard name
// the required rule's failure by its raw name.
func TestAFailureInARequiredRuleIsNamedByItsCode(t *testing.T) {
	required := testAnalyzer("sentinel", "compare errors with errors.Is")
	required.Run = func(*analysis.Pass) (any, error) { panic("boom") }
	requiring := testAnalyzer("nopanic", "report panics in library code")
	requiring.Requires = []*analysis.Analyzer{required}
	module := Module{Path: errsPath, Version: "v1.4.0", Namespace: "errs", Analyzers: []*analysis.Analyzer{requiring, required}}
	resolved, err := resolve([]Module{module}, Config{Modules: []ModuleConfig{errsConfig("errs_*")}})
	if err != nil {
		t.Fatal(err)
	}
	var stopped []failure

	resolved.register(func(f failure) { stopped = append(stopped, f) })
	_, _ = required.Run(&analysis.Pass{Analyzer: required, Pkg: types.NewPackage("example.com/a", "a")})

	if len(stopped) != 1 || stopped[0].Code != "errs_sentinel" || stopped[0].Source != errsPath+"@v1.4.0" {
		t.Errorf("stopped with %+v, want errs_sentinel from %s@v1.4.0", stopped, errsPath)
	}
}

func TestEachSetOfSettingsGetsItsOwnCache(t *testing.T) {
	base := t.TempDir()
	rule := &rule{Code: "errs_nopanic"}
	allowMust := []setting{{Rule: rule, Flag: "allow", Value: "Must"}}
	allowShould := []setting{{Rule: rule, Flag: "allow", Value: "Should"}}
	two := []setting{{Rule: rule, Flag: "allow", Value: "Must"}, {Rule: rule, Flag: "depth", Value: "2"}}
	twoReordered := []setting{two[1], two[0]}

	if settingsDigest(allowMust) == settingsDigest(allowShould) {
		t.Error("different values must use different caches")
	}
	if settingsDigest(two) != settingsDigest(twoReordered) {
		t.Error("the order settings arrive in must not matter")
	}

	t.Setenv("STATICCHECK_CACHE", base)
	if err := settingsCache(nil); err != nil || os.Getenv("STATICCHECK_CACHE") != base {
		t.Errorf("no settings must keep the shared cache: %q %v", os.Getenv("STATICCHECK_CACHE"), err)
	}
	if err := settingsCache(allowMust); err != nil || os.Getenv("STATICCHECK_CACHE") != filepath.Join(base, "settings-"+settingsDigest(allowMust)) {
		t.Errorf("STATICCHECK_CACHE = %q, %v", os.Getenv("STATICCHECK_CACHE"), err)
	}
	t.Setenv("STATICCHECK_CACHE", "off")
	if err := settingsCache(allowMust); err != nil || os.Getenv("STATICCHECK_CACHE") != "off" {
		t.Errorf("an explicit off must stay off: %q", os.Getenv("STATICCHECK_CACHE"))
	}
}
