package verify

import (
	"context"
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const testBaselinePath = ".levenshtein/baseline.json"

func lintCheck(id, dir string) PlannedCheck {
	return PlannedCheck{
		ID:          id,
		Check:       Check{Kind: CheckGoLint, Target: id, Environment: "host"},
		Target:      Target{Dir: dir, Workspace: ".", Inputs: []string{"."}},
		Environment: Environment{Executor: ExecutorNative},
	}
}

func lintFinding(file string, line int, code, message string) finding {
	return finding{Code: code, Message: message, Location: location{File: file, Line: line, Column: 2}}
}

func failedResult(id string, findings ...finding) Result {
	return Result{ID: id, Status: StatusFailed, Error: "Go policy lint failed", Details: findingsDetails(findings)}
}

func reportOf(checks []PlannedCheck, results ...Result) Report {
	return Report{Version: 1, Run: "branch", Status: reportStatus(results), Plan: Plan{Version: 1, Run: "branch", Checks: checks}, Results: results}
}

func lintEntry(file, code, message string, count int) BaselineEntry {
	return BaselineEntry{Kind: CheckGoLint, Dir: ".", File: file, Code: code, Message: message, Count: count}
}

// configOf is a configuration that declares exactly the given checks, each
// with a target and an environment named after its ID, and the rule modules
// they run. A go-lint check that runs none opts out of them.
func configOf(checks ...PlannedCheck) Config {
	cfg := Config{
		Version:      1,
		Targets:      map[string]Target{},
		Environments: map[string]Environment{},
		Checks:       map[string]Check{},
		RuleModules:  map[string]RuleModule{},
	}
	for _, planned := range checks {
		lint := planned.Check.Lint
		if planned.Check.Kind == CheckGoLint && len(planned.RuleModules) == 0 {
			lint = &LintCheck{Checks: planned.Check.lintChecks(), RuleModules: new(false)}
		}
		cfg.Checks[planned.ID] = Check{Kind: planned.Check.Kind, Target: planned.ID, Environment: planned.ID, Lint: lint}
		cfg.Targets[planned.ID] = planned.Target
		cfg.Environments[planned.ID] = planned.Environment
		for _, module := range planned.RuleModules {
			cfg.RuleModules[module.Path] = RuleModule{Version: module.Version, Namespace: module.Namespace, Select: module.Select}
		}
	}
	return cfg
}

// applyWith and recordWith judge a report against a configuration that declares the
// report's checks and any others given.
func applyWith(b Baseline, report Report, others ...PlannedCheck) Report {
	return b.Apply(report, configOf(append(slices.Clone(report.Plan.Checks), others...)...))
}

func recordWith(b Baseline, report Report, others ...PlannedCheck) (Baseline, BaselineChange, error) {
	return b.Record(report, configOf(append(slices.Clone(report.Plan.Checks), others...)...))
}

func findingsOf(t *testing.T, result Result) []finding {
	t.Helper()
	return detailFindings(result.Details)
}

func TestNormalizeMessage(t *testing.T) {
	t.Parallel()
	for in, want := range map[string]string{
		"unchecked error":                            "unchecked error",
		"  two\tspaces \n and a newline ":            "two spaces and a newline",
		"x declared at config.go:12:3 is unused":     "x declared at config.go is unused",
		"shadows the declaration at a/b.go:7":        "shadows the declaration at a/b.go",
		"version 1.27:1 is not a Go source position": "version 1.27:1 is not a Go source position",
	} {
		if got := normalizeMessage(in); got != want {
			t.Errorf("normalizeMessage(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBaselineAcceptsRecordedFindingsWhereverTheyMove(t *testing.T) {
	t.Parallel()
	check := lintCheck("lint", ".")
	baseline := Baseline{Path: testBaselinePath, Entries: []BaselineEntry{lintEntry("a.go", "errcheck", "unchecked error", 2)}}

	// The recorded findings moved to new lines; a message mentioning a line elsewhere still matches.
	report := reportOf([]PlannedCheck{check}, failedResult("lint",
		lintFinding("a.go", 40, "errcheck", "unchecked error"),
		lintFinding("a.go", 90, "errcheck", "unchecked  error"),
	))
	applied := applyWith(baseline, report)

	if applied.Status != StatusPassed || applied.Results[0].Status != StatusPassed || applied.Results[0].Error != "" {
		t.Fatalf("fully baselined run must pass: %+v", applied)
	}
	for _, f := range findingsOf(t, applied.Results[0]) {
		if !f.Baselined {
			t.Errorf("finding not marked baselined: %+v", f)
		}
	}
	if summary := *applied.Baseline; summary.File != testBaselinePath || summary.Baselined != 2 || summary.Stale != 0 || summary.Orphaned != nil {
		t.Fatalf("summary = %+v", applied.Baseline)
	}
	if report.Results[0].Status != StatusFailed || findingsOf(t, report.Results[0])[0].Baselined {
		t.Fatal("Apply changed the report it was given")
	}
}

func TestBaselineFailsOnNewFindings(t *testing.T) {
	check := lintCheck("lint", ".")
	baseline := Baseline{Path: testBaselinePath, Entries: []BaselineEntry{lintEntry("a.go", "errcheck", "unchecked error", 1)}}

	for name, findings := range map[string][]finding{
		"another occurrence than the count": {lintFinding("a.go", 1, "errcheck", "unchecked error"), lintFinding("a.go", 2, "errcheck", "unchecked error")},
		"another code":                      {lintFinding("a.go", 1, "errcheck", "unchecked error"), lintFinding("a.go", 1, "SA5001", "check the error first")},
		"another file":                      {lintFinding("a.go", 1, "errcheck", "unchecked error"), lintFinding("b.go", 1, "errcheck", "unchecked error")},
		"another message":                   {lintFinding("a.go", 1, "errcheck", "unchecked error"), lintFinding("a.go", 3, "errcheck", "unchecked error in Close")},
	} {
		t.Run(name, func(t *testing.T) {
			applied := applyWith(baseline, reportOf([]PlannedCheck{check}, failedResult("lint", findings...)))

			result := applied.Results[0]
			if applied.Status != StatusFailed || result.Status != StatusFailed || result.Error != "Go policy lint failed" {
				t.Fatalf("new finding must fail: %+v", result)
			}
			marked := 0
			for _, f := range findingsOf(t, result) {
				if f.Baselined {
					marked++
				}
			}
			if marked != 1 || applied.Baseline.Baselined != 1 {
				t.Fatalf("want exactly one baselined finding, got %d (%+v)", marked, applied.Baseline)
			}
		})
	}
}

func TestBaselineReportsStaleEntries(t *testing.T) {
	check := lintCheck("lint", ".")
	data := []byte(`{
  "version": 1,
  "findings": [
    {"kind": "go-lint", "dir": ".", "file": "a.go", "code": "errcheck", "message": "unchecked error", "count": 2},
    {"kind": "go-lint", "dir": ".", "file": "b.go", "code": "SA5001", "message": "check the error first", "count": 1}
  ]
}
`)
	entries, lines, err := parseBaseline(data)
	if err != nil {
		t.Fatal(err)
	}
	baseline := Baseline{Path: testBaselinePath, Entries: entries, lines: lines}

	t.Run("a fixed finding", func(t *testing.T) {
		applied := applyWith(baseline, reportOf([]PlannedCheck{check}, failedResult("lint",
			lintFinding("a.go", 1, "errcheck", "unchecked error"),
			lintFinding("b.go", 1, "SA5001", "check the error first"),
		)))

		result := applied.Results[0]
		if result.Status != StatusFailed || !strings.Contains(result.Error, "baseline entries") || applied.Baseline.Stale != 1 {
			t.Fatalf("a stale entry must fail the check: %+v %+v", result, applied.Baseline)
		}
		var stale []finding
		for _, f := range findingsOf(t, result) {
			if f.Code == baselineStaleCode {
				stale = append(stale, f)
			}
		}
		if len(stale) != 1 || stale[0].Location != (location{File: testBaselinePath, Line: 4}) || !strings.Contains(stale[0].Message, "1 of 2 baselined errcheck findings in a.go") {
			t.Fatalf("stale finding = %+v", stale)
		}
	})

	t.Run("a passing or cached check", func(t *testing.T) {
		applied := applyWith(baseline, reportOf([]PlannedCheck{check}, Result{ID: "lint", Status: StatusPassed, Cache: CacheInfo{Status: CacheHit}}))

		result := applied.Results[0]
		if result.Status != StatusFailed || applied.Baseline.Stale != 2 || len(findingsOf(t, result)) != 2 {
			t.Fatalf("every entry of a clean check is stale: %+v", result)
		}
		if lines := []int{findingsOf(t, result)[0].Location.Line, findingsOf(t, result)[1].Location.Line}; lines[0] != 4 || lines[1] != 5 {
			t.Fatalf("stale findings must point at their entries in order, got lines %v", lines)
		}
	})

	t.Run("checks that did not judge the entries", func(t *testing.T) {
		vet := lintCheck("vet", ".")
		vet.Check.Kind = CheckGoVet
		other := lintCheck("other", "services/api")
		report := reportOf([]PlannedCheck{check, vet, other},
			Result{ID: "lint", Status: StatusError, Error: "package discovery failed"},
			Result{ID: "vet", Status: StatusPassed},
			Result{ID: "other", Status: StatusPassed},
		)
		applied := applyWith(baseline, report)

		if applied.Baseline.Stale != 0 || applied.Results[0].Status != StatusError || applied.Results[1].Status != StatusPassed || applied.Results[2].Status != StatusPassed {
			t.Fatalf("only a completed check of the entry's kind and directory judges it: %+v", applied.Results)
		}
	})
}

func TestBaselineAcrossChecksOfOneDirectory(t *testing.T) {
	t.Parallel()
	native, dagger := lintCheck("native-lint", "."), lintCheck("lint", ".")
	baseline := Baseline{Path: testBaselinePath, Entries: []BaselineEntry{lintEntry("a.go", "gocognit", "too complex", 1)}}

	// Only one check selects gocognit. Each check matches on its own, and the
	// entry is stale only when every covering check leaves it unused.
	applied := applyWith(baseline, reportOf([]PlannedCheck{native, dagger},
		failedResult("native-lint", lintFinding("a.go", 1, "gocognit", "too complex")),
		Result{ID: "lint", Status: StatusPassed},
	))
	if applied.Status != StatusPassed || applied.Baseline.Stale != 0 || applied.Baseline.Baselined != 1 {
		t.Fatalf("an entry one check still matches is not stale: %+v", applied)
	}

	applied = applyWith(baseline, reportOf([]PlannedCheck{native, dagger}, Result{ID: "native-lint", Status: StatusPassed}, Result{ID: "lint", Status: StatusPassed}))
	if applied.Baseline.Stale != 1 || applied.Results[0].Status != StatusFailed || applied.Results[1].Status != StatusPassed {
		t.Fatalf("an entry no covering check matches is reported once, on the first: %+v", applied.Results)
	}
}

func TestBaselineKeepsOtherDetails(t *testing.T) {
	t.Parallel()
	check := lintCheck("lint", ".")
	details := json.RawMessage(`{"findings":[{"code":"errcheck","message":"unchecked error","location":{"file":"a.go","line":3,"column":1}}],"summary":{"kept":true}}`)
	baseline := Baseline{Path: testBaselinePath, Entries: []BaselineEntry{lintEntry("a.go", "errcheck", "unchecked error", 1)}}

	applied := applyWith(baseline, reportOf([]PlannedCheck{check}, Result{ID: "lint", Status: StatusFailed, Details: details}))

	var decoded struct {
		Summary  map[string]bool `json:"summary"`
		Findings []finding       `json:"findings"`
	}
	if err := json.Unmarshal(applied.Results[0].Details, &decoded); err != nil || !decoded.Summary["kept"] || !decoded.Findings[0].Baselined {
		t.Fatalf("details = %s (%v)", applied.Results[0].Details, err)
	}
}

func TestRecordBaseline(t *testing.T) {
	t.Parallel()
	lint, api := lintCheck("lint", "."), lintCheck("lint-api", "services/api")
	vet := lintCheck("vet", ".")
	vet.Check.Kind = CheckGoVet
	// A configured check the run leaves out covers the worker's entry.
	worker := lintCheck("lint-worker", "services/worker")
	existing := Baseline{Path: testBaselinePath, Entries: []BaselineEntry{
		lintEntry("a.go", "errcheck", "fixed since", 1),
		{Kind: CheckGoLint, Dir: "services/worker", File: "services/worker/w.go", Code: "SA5001", Message: "kept", Count: 2},
	}}
	report := reportOf([]PlannedCheck{lint, api, vet},
		failedResult("lint", lintFinding("z.go", 9, "LV1001", "use a type"), lintFinding("b.go", 1, "errcheck", "unchecked error"), lintFinding("b.go", 7, "errcheck", "unchecked  error")),
		failedResult("lint-api", lintFinding("services/api/x.go", 1, "SA5001", "check first at x.go:3")),
		failedResult("vet", finding{Code: "go-vet", Message: "x.go:1: bad", Location: location{File: ".", Line: 1}}),
	)

	recorded, change, err := recordWith(existing, report, worker)
	if err != nil {
		t.Fatal(err)
	}

	want := []BaselineEntry{
		lintEntry("b.go", "errcheck", "unchecked error", 2),
		lintEntry("z.go", "LV1001", "use a type", 1),
		{Kind: CheckGoLint, Dir: "services/api", File: "services/api/x.go", Code: "SA5001", Message: "check first at x.go", Count: 1},
		{Kind: CheckGoLint, Dir: "services/worker", File: "services/worker/w.go", Code: "SA5001", Message: "kept", Count: 2},
	}
	if len(recorded.Entries) != len(want) {
		t.Fatalf("entries = %+v", recorded.Entries)
	}
	for i := range want {
		if recorded.Entries[i] != want[i] {
			t.Errorf("entry %d = %+v, want %+v", i, recorded.Entries[i], want[i])
		}
	}
	if change.Added != 4 || change.Removed != 1 || strings.Join(change.Unrecorded, ",") != "vet" {
		t.Fatalf("change = %+v", change)
	}

	// Recording what the file already says changes nothing.
	again, change, err := recordWith(recorded, report, worker)
	if err != nil || change.Added != 0 || change.Removed != 0 || len(again.Entries) != len(want) {
		t.Fatalf("second record: %+v %+v %v", again.Entries, change, err)
	}

	// The recorded baseline accepts the run it came from.
	if applied := applyWith(recorded, reportOf([]PlannedCheck{lint, api}, report.Results[0], report.Results[1]), worker); applied.Status != StatusPassed {
		t.Fatalf("a run must pass the baseline it recorded: %+v", applied.Results)
	}
}

// unproducible is a go-lint check that could not have reported one baselined
// code. It still runs SA5001, whose entry is fixed, so an exemption that
// reached past that code would hide a stale entry. The configuration also
// declares a Dagger check the run leaves out, which could report the code.
type unproducible struct {
	check PlannedCheck
	code  string
}

func producer() PlannedCheck {
	check := lintCheck("lint-dagger", ".")
	check.Environment.Executor = ExecutorDagger
	check.RuleModules = []PlannedRuleModule{{Path: "example.com/errs", Version: "v1.0.0", Namespace: "errs", Select: []string{"errs_*"}}}
	return check
}

func unproducibleCases() map[string]unproducible {
	skipped := lintCheck("lint", ".")
	skipped.RuleModules = []PlannedRuleModule{{Path: "example.com/errs", Version: "v1.0.0", Namespace: "errs", Select: []string{"errs_*"}}}
	disabled := lintCheck("lint", ".")
	disabled.Check.Lint = &LintCheck{Checks: []string{"-unparam"}}
	return map[string]unproducible{
		"rule modules skipped": {check: skipped, code: "errs_nopanic"},
		"rule turned off":      {check: disabled, code: "unparam"},
	}
}

func (u unproducible) baseline() Baseline {
	return Baseline{Path: testBaselinePath, Entries: []BaselineEntry{
		lintEntry("a.go", "SA5001", "fixed since", 1),
		lintEntry("a.go", u.code, "still there", 1),
	}}
}

// report is a passing run of the check, with the warning a native check that
// skipped its rule modules gives.
func (u unproducible) report() Report {
	result := Result{ID: "lint", Status: StatusPassed, Warnings: skippedRuleModules(Request{PlannedCheck: u.check})}
	return reportOf([]PlannedCheck{u.check}, result)
}

func TestBaselineIgnoresEntriesACheckCouldNotProduce(t *testing.T) {
	t.Parallel()
	for name, tc := range unproducibleCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			applied := applyWith(tc.baseline(), tc.report(), producer())

			stale := findingsOf(t, applied.Results[0])
			if applied.Baseline.Stale != 1 || len(stale) != 1 || !strings.Contains(stale[0].Message, "SA5001") {
				t.Fatalf("only the SA5001 entry is stale: %+v %+v", applied.Baseline, stale)
			}
		})
	}
}

func TestRecordBaselineKeepsEntriesACheckCouldNotProduce(t *testing.T) {
	t.Parallel()
	for name, tc := range unproducibleCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			recorded, change, err := recordWith(tc.baseline(), tc.report(), producer())
			if err != nil {
				t.Fatal(err)
			}

			if len(recorded.Entries) != 1 || recorded.Entries[0].Code != tc.code || change.Removed != 1 {
				t.Fatalf("only the SA5001 entry is removed: %+v %+v", recorded.Entries, change)
			}
		})
	}
}

// orphanCase is a configuration none of whose checks could report the last
// entry of a baseline, and a run of part of it that reached a verdict.
type orphanCase struct {
	configured []PlannedCheck
	run        []PlannedCheck
	entry      BaselineEntry
}

func daggerLint(id, dir string, selected ...string) PlannedCheck {
	check := lintCheck(id, dir)
	check.Environment.Executor = ExecutorDagger
	if len(selected) != 0 {
		check.RuleModules = []PlannedRuleModule{{Path: "example.com/errs", Version: "v1.0.0", Namespace: "errs", Select: selected}}
	}
	return check
}

func orphanCases() map[string]orphanCase {
	noModules := daggerLint("lint", ".")
	dropped := daggerLint("lint", ".", "errs_other")
	offHere, offThere := daggerLint("lint", "."), lintCheck("native-lint", ".")
	offHere.Check.Lint = &LintCheck{Checks: []string{"-unparam"}}
	offThere.Check.Lint = &LintCheck{Checks: []string{"all", "-unparam"}}
	shell := lintCheck("shell", ".")
	shell.Check.Kind = CheckShellLint
	vet := lintCheck("vet", ".")
	vet.Check.Kind = CheckGoVet

	return map[string]orphanCase{
		"rule module removed": {
			configured: []PlannedCheck{noModules},
			run:        []PlannedCheck{noModules},
			entry:      lintEntry("a.go", "errs_nopanic", "panics", 1),
		},
		"rule dropped from the module's select": {
			configured: []PlannedCheck{dropped},
			run:        []PlannedCheck{dropped},
			entry:      lintEntry("a.go", "errs_nopanic", "panics", 1),
		},
		"rule off in every go-lint check": {
			configured: []PlannedCheck{offHere, offThere},
			run:        []PlannedCheck{offHere, offThere},
			entry:      lintEntry("a.go", "unparam", "result 0 is always nil", 1),
		},
		"check removed": {
			configured: []PlannedCheck{offHere, shell},
			run:        []PlannedCheck{shell},
			entry:      BaselineEntry{Kind: CheckGoLint, Dir: "services/gone", File: "services/gone/x.go", Code: "SA5001", Message: "check first", Count: 1},
		},
		// A run that covers no check of the entry's kind still reports it,
		// because no run could ever need it.
		"partial run": {
			configured: []PlannedCheck{offHere, vet},
			run:        []PlannedCheck{vet},
			entry:      BaselineEntry{Kind: CheckShellLint, Dir: ".", File: "run.sh", Code: "SC2086", Message: "quote it", Count: 1},
		},
	}
}

// baseline is a file whose first entry the configured go-lint check on "."
// could produce and the run did not judge, and whose second, on line 5, is
// the orphan.
func (o orphanCase) baseline(t *testing.T) Baseline {
	t.Helper()
	kept, err := json.Marshal(lintEntry("kept.go", "errcheck", "unchecked error", 1))
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := json.Marshal(o.entry)
	if err != nil {
		t.Fatal(err)
	}
	data := "{\n  \"version\": 1,\n  \"findings\": [\n    " + string(kept) + ",\n    " + string(orphan) + "\n  ]\n}\n"

	entries, lines, err := parseBaseline([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return Baseline{Path: testBaselinePath, Entries: entries, lines: lines}
}

// report is a run in which every go-lint check on "." found the kept entry's
// finding and every other check passed, so only the orphan can fail it.
func (o orphanCase) report() Report {
	var results []Result
	for _, check := range o.run {
		result := Result{ID: check.ID, Status: StatusPassed}
		if check.Check.Kind == CheckGoLint && check.Target.Dir == "." {
			result = failedResult(check.ID, lintFinding("kept.go", 1, "errcheck", "unchecked error"))
		}
		results = append(results, result)
	}
	return reportOf(o.run, results...)
}

func TestBaselineReportsOrphanedEntries(t *testing.T) {
	t.Parallel()
	for name, tc := range orphanCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			applied := tc.baseline(t).Apply(tc.report(), configOf(tc.configured...))

			if applied.Status != StatusFailed || applied.Baseline.Stale != 1 || len(applied.Baseline.Orphaned) != 1 {
				t.Fatalf("an orphaned entry must fail the run: %s %+v", applied.Status, applied.Baseline)
			}
			orphan := applied.Baseline.Orphaned[0]
			if orphan.Code != baselineStaleCode || orphan.Location != (location{File: testBaselinePath, Line: 5}) {
				t.Fatalf("orphan finding = %+v", orphan)
			}
			for _, want := range []string{tc.entry.Code, tc.entry.File, tc.entry.Message, "verify branch --write-baseline"} {
				if !strings.Contains(orphan.Message, want) {
					t.Errorf("message %q does not mention %q", orphan.Message, want)
				}
			}
			for _, result := range applied.Results {
				if slices.ContainsFunc(findingsOf(t, result), func(f finding) bool { return f.Code == baselineStaleCode }) {
					t.Errorf("an orphan belongs to no check, but %s reports it: %+v", result.ID, result)
				}
			}
		})
	}
}

func TestRecordBaselineRemovesOrphanedEntries(t *testing.T) {
	t.Parallel()
	for name, tc := range orphanCases() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			baseline := tc.baseline(t)
			recorded, change, err := baseline.Record(tc.report(), configOf(tc.configured...))
			if err != nil {
				t.Fatal(err)
			}

			if !slices.Equal(recorded.Entries, baseline.Entries[:1]) || change.Removed != 1 || change.Added != 0 {
				t.Fatalf("only the orphan is removed: %+v %+v", recorded.Entries, change)
			}
		})
	}
}

// An entry that a configured check could report is left to that check, even
// when the run leaves it out and no check of the run could report it.
func TestBaselineKeepsEntriesAnotherConfiguredCheckCouldProduce(t *testing.T) {
	t.Parallel()
	native := lintCheck("lint", ".")
	native.RuleModules = []PlannedRuleModule{{Path: "example.com/errs", Version: "v1.0.0", Namespace: "errs", Select: []string{"errs_*"}}}
	shell := lintCheck("shell", ".")
	shell.Check.Kind = CheckShellLint
	api := daggerLint("lint-api", "services/api", "errs_*")
	entries := []BaselineEntry{
		lintEntry("a.go", "errs_nopanic", "panics", 1),
		{Kind: CheckGoLint, Dir: "services/api", File: "services/api/a.go", Code: "errs_nopanic", Message: "panics", Count: 1},
		{Kind: CheckShellLint, Dir: ".", File: "run.sh", Code: "SC2086", Message: "quote it", Count: 1},
	}
	baseline := Baseline{Path: testBaselinePath, Entries: entries}
	cfg := configOf(native, api, shell, daggerLint("lint-dagger", ".", "errs_*"))
	report := reportOf([]PlannedCheck{native, api}, Result{ID: "lint", Status: StatusPassed, Warnings: skippedRuleModules(Request{PlannedCheck: native})}, failedResult("lint-api", lintFinding("services/api/a.go", 1, "errs_nopanic", "panics")))

	applied := baseline.Apply(report, cfg)
	if applied.Status != StatusPassed || applied.Baseline.Stale != 0 || len(applied.Baseline.Orphaned) != 0 {
		t.Fatalf("no entry is orphaned: %+v", applied.Baseline)
	}
	recorded, change, err := baseline.Record(report, cfg)
	if err != nil || len(recorded.Entries) != len(entries) || change.Removed != 0 {
		t.Fatalf("every entry is kept: %+v %+v %v", recorded.Entries, change, err)
	}
}

// A check declared with "targets" produces entries in each target's directory,
// and a check whose target is not declared produces none.
func TestConfiguredChecksExpandTargets(t *testing.T) {
	t.Parallel()
	cfg := Config{
		Targets:      map[string]Target{"app": {Dir: "."}, "api": {Dir: "services/api"}},
		Environments: map[string]Environment{"host": {Executor: ExecutorNative}},
		Checks: map[string]Check{
			"lint":   {Kind: CheckGoLint, Targets: []string{"app", "api"}, Environment: "host"},
			"broken": {Kind: CheckGoLint, Target: "missing", Environment: "host"},
		},
	}
	entry := func(dir string) BaselineEntry {
		return BaselineEntry{Kind: CheckGoLint, Dir: dir, File: path.Join(dir, "a.go"), Code: "SA5001", Message: "check first", Count: 1}
	}
	baseline := Baseline{Path: testBaselinePath, Entries: []BaselineEntry{entry("."), entry("services/api"), entry("services/gone")}}

	if got := baseline.orphaned(reportOf(nil), cfg); !slices.Equal(got, []int{2}) {
		t.Fatalf("orphaned entries = %v, want only the one in services/gone", got)
	}
}

// A check that did run the rule, on Dagger or with the rule turned back on,
// still judges and records its entries.
func TestBaselineJudgesEntriesACheckCouldProduce(t *testing.T) {
	t.Parallel()
	check := lintCheck("lint", ".")
	check.Environment.Executor = ExecutorDagger
	check.RuleModules = []PlannedRuleModule{{Path: "example.com/errs", Version: "v1.0.0", Namespace: "errs", Select: []string{"errs_*"}}}
	check.Check.Lint = &LintCheck{Checks: []string{"-unparam", "unparam", "-errs_other"}}
	baseline := Baseline{Path: testBaselinePath, Entries: []BaselineEntry{
		lintEntry("a.go", "SA5001", "fixed since", 1),
		lintEntry("a.go", "errs_nopanic", "fixed since", 1),
		lintEntry("a.go", "unparam", "fixed since", 1),
	}}
	report := reportOf([]PlannedCheck{check}, Result{ID: "lint", Status: StatusPassed})

	if applied := applyWith(baseline, report); applied.Baseline.Stale != 3 {
		t.Fatalf("every entry is stale: %+v", applied.Baseline)
	}
	recorded, change, err := recordWith(baseline, report)
	if err != nil || len(recorded.Entries) != 0 || change.Removed != 3 {
		t.Fatalf("every entry is removed: %+v %+v %v", recorded.Entries, change, err)
	}
}

// A Dagger check with rule modules runs rules no pattern names by their
// current code: the community linter's own lvrules_* codes, which every such
// check runs, and a rule a module still selects by its old name. Their
// entries still accept the findings and go stale when fixed.
func TestBaselineJudgesCommunityCodesNoPatternNames(t *testing.T) {
	check := lintCheck("lint", ".")
	check.Environment.Executor = ExecutorDagger
	check.RuleModules = []PlannedRuleModule{{Path: "example.com/errs", Version: "v1.0.0", Namespace: "errs", Select: []string{"errs_old"}}}
	baseline := Baseline{Path: testBaselinePath, Entries: []BaselineEntry{
		lintEntry("a.go", "errs_new", "renamed rule", 1),
		lintEntry("a.go", "lvrules_mixed", "mixed directive", 1),
		lintEntry("b.go", "lvrules_renamed", "fixed since", 1),
	}}
	found := failedResult("lint",
		lintFinding("a.go", 3, "errs_new", "renamed rule"),
		lintFinding("a.go", 5, "errs_new", "renamed rule"),
		lintFinding("a.go", 7, "lvrules_mixed", "mixed directive"),
	)
	report := reportOf([]PlannedCheck{check}, found)

	applied := applyWith(baseline, report)
	if applied.Baseline.Baselined != 2 || applied.Baseline.Stale != 1 {
		t.Fatalf("both entries accept their findings and the fixed one is stale: %+v", applied.Baseline)
	}
	recorded, _, err := recordWith(baseline, report)
	if err != nil {
		t.Fatal(err)
	}
	want := []BaselineEntry{lintEntry("a.go", "errs_new", "renamed rule", 2), lintEntry("a.go", "lvrules_mixed", "mixed directive", 1)}
	if !slices.Equal(recorded.Entries, want) {
		t.Fatalf("recorded %+v, want %+v", recorded.Entries, want)
	}
}

func TestBaselineLeavesAdvisoryFindingsAlone(t *testing.T) {
	t.Parallel()
	check := lintCheck("lint", ".")
	advisory := lintFinding("a.go", 4, "ACME001", "prefer the helper")
	advisory.Advisory = true
	report := reportOf([]PlannedCheck{check}, Result{ID: "lint", Status: StatusPassed, Details: findingsDetails([]finding{advisory})})

	recorded, change, err := recordWith(Baseline{Path: testBaselinePath}, report)
	if err != nil || len(recorded.Entries) != 0 || change.Added != 0 {
		t.Fatalf("an advisory finding must not be recorded: %+v %+v %v", recorded.Entries, change, err)
	}

	applied := applyWith(recorded, report)
	if applied.Status != StatusPassed || findingsOf(t, applied.Results[0])[0].Baselined {
		t.Fatalf("an advisory finding must pass unmarked: %+v", applied.Results)
	}
}

func TestRecordBaselineRefusesUnfinishedRuns(t *testing.T) {
	t.Parallel()
	lint, other := lintCheck("lint", "."), lintCheck("other", "x")
	for _, status := range []Status{StatusError, StatusIncomplete, StatusCancelled} {
		report := reportOf([]PlannedCheck{lint, other}, failedResult("lint", lintFinding("a.go", 1, "errcheck", "e")), Result{ID: "other", Status: status})
		if _, _, err := recordWith(Baseline{Path: testBaselinePath}, report); err == nil || !strings.Contains(err.Error(), "other ("+string(status)+")") {
			t.Errorf("%s: error = %v", status, err)
		}
	}
	if _, _, err := recordWith(Baseline{Path: testBaselinePath}, reportOf(nil)); err == nil {
		t.Error("a run without checks must not write a baseline")
	}
}

func TestBaselineFileRoundTrip(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	baseline := Baseline{Path: testBaselinePath, Entries: []BaselineEntry{
		lintEntry("a.go", "errcheck", "error of <Close> & friends", 1),
		lintEntry("b.go", "SA5001", "check \"err\" first", 3),
	}}
	if err := baseline.Write(source); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(source, testBaselinePath))
	if err != nil {
		t.Fatal(err)
	}
	want := `{
  "version": 1,
  "findings": [
    {"kind":"go-lint","dir":".","file":"a.go","code":"errcheck","message":"error of <Close> & friends","count":1},
    {"kind":"go-lint","dir":".","file":"b.go","code":"SA5001","message":"check \"err\" first","count":3}
  ]
}
`
	if string(data) != want {
		t.Fatalf("file:\n%s\nwant:\n%s", data, want)
	}
	if info, err := os.Stat(filepath.Join(source, testBaselinePath)); err != nil || info.Mode().Perm() != 0644 {
		t.Fatalf("mode: %v %v", info, err)
	}

	loaded, err := LoadBaseline(source, testBaselinePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Entries) != 2 || loaded.Entries[1] != baseline.Entries[1] || loaded.lines[0] != 4 || loaded.lines[1] != 5 {
		t.Fatalf("loaded %+v lines %v", loaded.Entries, loaded.lines)
	}

	empty, err := (Baseline{Path: testBaselinePath}).Encode()
	if err != nil || string(empty) != "{\n  \"version\": 1,\n  \"findings\": []\n}\n" {
		t.Fatalf("empty baseline = %q %v", empty, err)
	}
}

func TestLoadBaseline(t *testing.T) {
	source := t.TempDir()
	if baseline, err := LoadBaseline(source, testBaselinePath); err != nil || len(baseline.Entries) != 0 || baseline.Path != testBaselinePath {
		t.Fatalf("a missing file records nothing: %+v %v", baseline, err)
	}

	valid := `{"kind":"go-lint","dir":".","file":"a.go","code":"errcheck","message":"e","count":1}`
	for name, tc := range map[string]struct {
		body string
		want string
	}{
		"version":          {`{"version": 2, "findings": []}`, `"version": 1`},
		"unknown field":    {`{"version": 1, "findings": [], "extra": 1}`, "unknown field"},
		"unsupported kind": {`{"version": 1, "findings": [{"kind":"go-vet","dir":".","file":"a.go","code":"go-vet","message":"e","count":1}]}`, `kind "go-vet" cannot be baselined; only go-http, go-imports, go-lint, go-sql, shell-lint`},
		"duplicate":        {`{"version": 1, "findings": [` + valid + `,` + valid + `]}`, "entry 2 repeats"},
		"escaping file":    {`{"version": 1, "findings": [{"kind":"go-lint","dir":".","file":"../a.go","code":"c","message":"e","count":1}]}`, `file "../a.go"`},
		"zero count":       {`{"version": 1, "findings": [` + valid + `,{"kind":"go-lint","dir":".","file":"a.go","code":"c","message":"e","count":0}]}`, "entry 2: count must be at least 1"},
		"no message":       {`{"version": 1, "findings": [{"kind":"go-lint","dir":".","file":"a.go","code":"c","message":" ","count":1}]}`, "needs a code and a message"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := os.MkdirAll(filepath.Join(source, ".levenshtein"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, testBaselinePath), []byte(tc.body), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadBaseline(source, testBaselinePath); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
		})
	}

	for _, path := range []string{"", ".", "../baseline.json", "/tmp/baseline.json", ".env"} {
		if _, err := LoadBaseline(source, path); err == nil {
			t.Errorf("path %q accepted", path)
		}
	}
}

func TestBaselineWriteRefusesSymlinks(t *testing.T) {
	t.Parallel()
	source, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(source, ".levenshtein")); err != nil {
		t.Fatal(err)
	}
	if err := (Baseline{Path: testBaselinePath}).Write(source); err == nil {
		t.Fatalf("write through a symlink: %v", err)
	}
}

func TestPlanValidatesBaselinePath(t *testing.T) {
	t.Parallel()
	source := t.TempDir()
	cfg := Config{
		Version:      1,
		Targets:      map[string]Target{"app": {Dir: ".", Inputs: []string{"."}}},
		Environments: map[string]Environment{"host": {Executor: ExecutorNative}},
		Checks:       map[string]Check{"lint": {Kind: CheckGoLint, Target: "app", Environment: "host"}},
		Runs:         map[string]Run{"branch": {Checks: []string{"lint"}}},
		Baseline:     testBaselinePath,
	}
	plan, err := cfg.Plan(source, "branch")
	if err != nil || plan.Baseline != testBaselinePath {
		t.Fatalf("plan = %+v, %v", plan, err)
	}

	cfg.Baseline = "../baseline.json"
	if _, err := cfg.Plan(source, "branch"); err == nil || !strings.Contains(err.Error(), "baseline file") {
		t.Fatalf("escaping baseline accepted: %v", err)
	}
}

type findingExecutor struct {
	calls int
}

func (e *findingExecutor) Execute(context.Context, Request) Result {
	e.calls++
	return failedResult("", lintFinding("a.go", 1, "errcheck", "unchecked error"))
}

// A check whose findings are all baselined still fails underneath, so the
// result cache never stores it: every run executes it again, fresh, and the
// baseline reaches the same verdict each time.
func TestBaselinedCheckIsExecutedOnEveryRun(t *testing.T) {
	t.Parallel()
	req := cacheRequest(t)
	executor := &findingExecutor{}
	runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: executor}
	baseline := Baseline{Path: testBaselinePath, Entries: []BaselineEntry{lintEntry("a.go", "errcheck", "unchecked error", 1)}}

	for run, wantCache := range []CacheStatus{CacheMiss, CacheFresh, CacheFresh} {
		result := runner.Execute(context.Background(), req)
		result.ID = "lint"
		applied := applyWith(baseline, reportOf([]PlannedCheck{lintCheck("lint", ".")}, result))

		if executor.calls != run+1 || result.Status != StatusFailed || result.Cache.Status != wantCache {
			t.Fatalf("run %d: calls %d, raw result %+v", run, executor.calls, result)
		}
		if applied.Status != StatusPassed {
			t.Fatalf("run %d: baselined result must pass: %+v", run, applied.Results[0])
		}
	}
}

// entryLines only locates entries for stale findings; a file with more
// entries than it was asked about must not cost more than the extra lines.
func TestEntryLinesStopsAtTheCountItWasGiven(t *testing.T) {
	t.Parallel()
	data := []byte("{\n  \"version\": 1,\n  \"findings\": [\n    {\"code\": \"a\"},\n    {\"code\": \"b\"}\n  ]\n}\n")

	if got := entryLines(data, 2); !slices.Equal(got, []int{4, 5}) {
		t.Fatalf("lines of two entries: %v", got)
	}
	if got := entryLines(data, 1); !slices.Equal(got, []int{4}) {
		t.Fatalf("lines of the first entry only: %v", got)
	}
}
