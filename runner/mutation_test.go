package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The reports under testdata/mutation/reports are gremlins v0.6.0 output for
// the fixtures beside them, recorded with the runner's flags. timedout.json is
// weak.json with one KILLED status changed to TIMED OUT, because a live
// timeout cannot be produced on demand.
func report(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "mutation", "reports", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func sources(t *testing.T, fixture string, files ...string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join("testdata", "mutation", fixture, filepath.FromSlash(file)))
		if err != nil {
			t.Fatal(err)
		}
		out[file] = strings.Split(string(data), "\n")
	}
	return out
}

// reported is a run whose limit, 2s times 10, clears mutantTimeFloor.
func reported(body string) mutationRun {
	return mutationRun{Report: body, Reported: true, Stdout: "Starting...\nGathering coverage... done in 2s\n", Stderr: "go: no module dependencies to download\n", Coefficient: gremlinsTimeoutCoef}
}

func codes(findings []diagnostic) []string {
	var out []string
	for _, finding := range findings {
		out = append(out, finding.Code)
	}
	return out
}

func TestDecideMutationReportsEachSurvivorWithItsLine(t *testing.T) {
	in := mutationInput{Module: "app", Files: []string{"clamp.go"}, Sources: sources(t, "weak", "clamp.go")}

	verdict, err := decideMutation(reported(report(t, "weak")), in)
	if err != nil {
		t.Fatal(err)
	}

	if len(verdict.Findings) != 2 || verdict.Incomplete {
		t.Fatalf("want two survivors and a complete run, got %+v", verdict)
	}
	for i, want := range []struct {
		line int
		text string
	}{{5, "if n < lo {"}, {8, "if n > hi {"}} {
		finding := verdict.Findings[i]
		if finding.Code != "go-mutation" || finding.Location.File != "app/clamp.go" || finding.Location.Line != want.line || finding.Location.Column != 7 {
			t.Errorf("finding %d = %+v, want go-mutation at app/clamp.go:%d:7", i, finding, want.line)
		}
		if !strings.Contains(finding.Message, "CONDITIONALS_BOUNDARY") || !strings.Contains(finding.Message, want.text) {
			t.Errorf("finding %d message %q should name the mutator and quote %q", i, finding.Message, want.text)
		}
	}
	if verdict.Summary.Killed != 2 || verdict.Summary.Lived != 2 {
		t.Errorf("summary = %+v, want 2 killed and 2 lived", verdict.Summary)
	}
}

func TestDecideMutationPassesWhenEveryMutantIsKilled(t *testing.T) {
	in := mutationInput{Module: ".", Files: []string{"add.go"}, Sources: sources(t, "strong", "add.go")}

	verdict, err := decideMutation(reported(report(t, "strong")), in)
	if err != nil {
		t.Fatal(err)
	}

	if len(verdict.Findings) != 0 || verdict.Summary.Killed != 1 {
		t.Fatalf("want a pass with one killed mutant, got %+v", verdict)
	}
}

func TestDecideMutationAcceptsListedSurvivorsByLineText(t *testing.T) {
	accepted, err := os.ReadFile(filepath.Join("testdata", "mutation", "accepted", ".levenshtein", "mutation-accepted.json"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parseAccepted(string(accepted), true)
	if err != nil {
		t.Fatal(err)
	}
	in := mutationInput{Module: ".", Files: []string{"clamp.go"}, Sources: sources(t, "accepted", "clamp.go"), Accepted: entries}

	verdict, err := decideMutation(reported(report(t, "accepted")), in)
	if err != nil {
		t.Fatal(err)
	}

	if len(verdict.Findings) != 0 || verdict.Summary.Accepted != 2 || verdict.Summary.Lived != 0 {
		t.Fatalf("both boundary survivors are accepted, got %+v", verdict)
	}
}

func TestDecideMutationSurvivesLineShifts(t *testing.T) {
	// Two blank lines above the function move every mutant down by two; the
	// entries still match because they name the text, not the number.
	shifted := sources(t, "accepted", "clamp.go")
	shifted["clamp.go"] = append([]string{"", ""}, shifted["clamp.go"]...)
	body := strings.NewReplacer(`"line":5`, `"line":7`, `"line":8`, `"line":10`).Replace(report(t, "accepted"))
	entries := []acceptedEntry{
		{File: "clamp.go", Mutator: "CONDITIONALS_BOUNDARY", Line: "if n < lo {", Reason: "equivalent"},
		{File: "clamp.go", Mutator: "CONDITIONALS_BOUNDARY", Line: "if n > hi {", Reason: "equivalent"},
	}

	verdict, err := decideMutation(reported(body), mutationInput{Module: ".", Files: []string{"clamp.go"}, Sources: shifted, Accepted: entries})
	if err != nil {
		t.Fatal(err)
	}

	if len(verdict.Findings) != 0 {
		t.Fatalf("shifted lines must still match their entries: %+v", verdict.Findings)
	}
}

func TestDecideMutationFlagsStaleEntriesOnlyForMutatedFiles(t *testing.T) {
	text := "{\n  \"version\": 1,\n  \"accepted\": [\n    {\"file\": \"clamp.go\", \"mutator\": \"CONDITIONALS_BOUNDARY\", \"line\": \"if n <= 0 {\", \"reason\": \"old\"},\n    {\"file\": \"other.go\", \"mutator\": \"ARITHMETIC_BASE\", \"line\": \"return a + b\", \"reason\": \"not mutated this run\"}\n  ]\n}\n"
	entries, err := parseAccepted(text, true)
	if err != nil {
		t.Fatal(err)
	}
	in := mutationInput{Module: ".", Files: []string{"add.go", "clamp.go"}, Sources: sources(t, "accepted", "clamp.go"), Accepted: entries, AcceptedPath: ".levenshtein/mutation-accepted.json", AcceptedText: text}

	verdict, err := decideMutation(reported(report(t, "accepted")), in)
	if err != nil {
		t.Fatal(err)
	}

	stale := slices.IndexFunc(verdict.Findings, func(d diagnostic) bool { return d.Code == "go-mutation-stale" })
	if stale < 0 || strings.Count(strings.Join(codes(verdict.Findings), " "), "go-mutation-stale") != 1 {
		t.Fatalf("want exactly one stale entry, for clamp.go: %v", codes(verdict.Findings))
	}
	if got := verdict.Findings[stale].Location; got.File != ".levenshtein/mutation-accepted.json" || got.Line != 4 {
		t.Errorf("stale finding at %+v, want the entry's line 4 in the accepted file", got)
	}
}

func TestDecideMutationCountsATimeoutAsCaught(t *testing.T) {
	in := mutationInput{Module: ".", Files: []string{"clamp.go"}, Sources: sources(t, "weak", "clamp.go")}

	verdict, err := decideMutation(reported(report(t, "timedout")), in)
	if err != nil {
		t.Fatal(err)
	}

	// A mutation that makes the code hang is one the tests noticed.
	if verdict.Incomplete || verdict.Summary.TimedOut != 1 || len(verdict.Summary.TimedOutMutants) != 1 {
		t.Fatalf("a timeout next to killed mutants is caught, not incomplete: %+v", verdict)
	}
	if got := codes(verdict.Findings); !slices.Equal(got, []string{"go-mutation", "go-mutation"}) {
		t.Errorf("findings = %v, want only the two survivors", got)
	}
}

func TestDecideMutationIsIncompleteOnlyWhenEveryCoveredMutantTimedOut(t *testing.T) {
	weak := report(t, "weak")
	in := mutationInput{Module: ".", Files: []string{"clamp.go"}, Sources: sources(t, "weak", "clamp.go")}
	// weak.json: KILLED and LIVED CONDITIONALS_NEGATION/BOUNDARY on lines 5 and 8.
	for _, tc := range []struct {
		name       string
		body       string
		incomplete bool
		codes      []string
	}{
		{
			name:       "all four covered mutants timed out",
			body:       strings.NewReplacer(`"KILLED"`, `"TIMED OUT"`, `"LIVED"`, `"TIMED OUT"`).Replace(weak),
			incomplete: true,
			codes:      []string{"go-mutation-timeout", "go-mutation-timeout", "go-mutation-timeout", "go-mutation-timeout"},
		},
		{
			// The review's case: a deliberate hang next to a survivor, nothing
			// killed. The survivor is a normal finding, fixable with a test.
			name:  "hangs next to survivors",
			body:  strings.ReplaceAll(weak, `"KILLED"`, `"TIMED OUT"`),
			codes: []string{"go-mutation", "go-mutation"},
		},
		{
			// Two deterministic hangs are the change's result, not a broken
			// machine, so the run passes rather than staying incomplete forever.
			name: "too few timeouts to blame the machine",
			body: strings.NewReplacer(`"KILLED"`, `"TIMED OUT"`, `"LIVED"`, `"NOT COVERED"`).Replace(weak),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			verdict, err := decideMutation(reported(tc.body), in)
			if err != nil {
				t.Fatal(err)
			}

			if verdict.Incomplete != tc.incomplete {
				t.Errorf("incomplete = %v, want %v (%+v)", verdict.Incomplete, tc.incomplete, verdict.Summary)
			}
			if got := codes(verdict.Findings); !slices.Equal(got, tc.codes) {
				t.Errorf("findings = %v, want %v", got, tc.codes)
			}
		})
	}
}

// skippedOn marks every mutant on a line as gremlins does for a line outside
// its diff: skipped without running the tests.
func skippedOn(body string, lines ...int) string {
	for _, line := range lines {
		for _, status := range []string{"KILLED", "LIVED"} {
			body = strings.ReplaceAll(body, `"status":"`+status+`","line":`+strconv.Itoa(line)+`,`, `"status":"SKIPPED","line":`+strconv.Itoa(line)+`,`)
		}
	}
	return body
}

func TestDecideMutationFailsOnlyOnChangedLines(t *testing.T) {
	in := mutationInput{
		Module:  ".",
		Files:   []string{"clamp.go"},
		Lines:   map[string][]lineRange{"clamp.go": {{Start: 4, End: 6}}},
		Sources: sources(t, "weak", "clamp.go"),
	}

	verdict, err := decideMutation(reported(skippedOn(report(t, "weak"), 8)), in)
	if err != nil {
		t.Fatal(err)
	}

	if len(verdict.Findings) != 1 || verdict.Findings[0].Location.Line != 5 {
		t.Fatalf("only the survivor on changed line 5 fails: %+v", verdict.Findings)
	}
	if verdict.Summary.Lived != 1 || verdict.Summary.Unchanged != 0 || verdict.Summary.Skipped != 2 {
		t.Errorf("the line-8 mutants must be skipped, not run: %+v", verdict.Summary)
	}
}

func TestDecideMutationListsSurvivorsOnAnAcceptedEntrysUnchangedLine(t *testing.T) {
	// The entry names line 8, so its mutants run although the change did not
	// write that line: the entry is now caught, and the other mutant on the
	// line survives without failing the check.
	entries := []acceptedEntry{{File: "clamp.go", Mutator: "CONDITIONALS_NEGATION", Line: "if n > hi {", Reason: "old"}}
	in := mutationInput{
		Module:   ".",
		Files:    []string{"clamp.go"},
		Lines:    map[string][]lineRange{"clamp.go": {{Start: 4, End: 6}}},
		Sources:  sources(t, "weak", "clamp.go"),
		Accepted: entries,
	}

	verdict, err := decideMutation(reported(report(t, "weak")), in)
	if err != nil {
		t.Fatal(err)
	}

	if got := codes(verdict.Findings); !slices.Equal(got, []string{"go-mutation", "go-mutation-stale"}) {
		t.Fatalf("findings = %v, want the line-5 survivor and the caught entry", verdict.Findings)
	}
	if verdict.Summary.Unchanged != 1 || verdict.Summary.UnchangedList[0].Line != 8 || verdict.Summary.Skipped != 0 {
		t.Errorf("the line-8 survivor must be listed as unchanged: %+v", verdict.Summary)
	}
}

func TestDecideMutationRejectsARunThatIgnoredTheChangedLines(t *testing.T) {
	in := mutationInput{
		Module:  ".",
		Files:   []string{"clamp.go"},
		Lines:   map[string][]lineRange{"clamp.go": {{Start: 5, End: 5}}},
		Sources: sources(t, "weak", "clamp.go"),
	}
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		// A diff gremlins could not match to the file skips every mutant, and
		// an empty one runs them all; either would change the verdict unseen.
		{"a changed line skipped", skippedOn(report(t, "weak"), 5, 8), "skipped clamp.go:5"},
		{"an unchanged line run", report(t, "weak"), "ran clamp.go:8"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decideMutation(reported(tc.body), in)

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error mentioning %q, got %v", tc.want, err)
			}
		})
	}
}

func TestMutatedLinesAddsEveryLineAnAcceptedEntryNames(t *testing.T) {
	weak := sources(t, "weak", "clamp.go")
	in := mutationInput{
		Files:   []string{"clamp.go", "new.go"},
		Lines:   map[string][]lineRange{"clamp.go": {{Start: 5, End: 6}}},
		Sources: map[string][]string{"clamp.go": weak["clamp.go"], "new.go": {"package weak", "", "var x = 1", ""}},
		Accepted: []acceptedEntry{
			{File: "clamp.go", Mutator: "CONDITIONALS_BOUNDARY", Line: "if n > hi {"},
			{File: "clamp.go", Mutator: "CONDITIONALS_BOUNDARY", Line: "return n", Function: "Other"},
			{File: "clamp.go", Mutator: "CONDITIONALS_BOUNDARY", Line: "if n < lo {"},
			{File: "gone.go", Mutator: "CONDITIONALS_BOUNDARY", Line: "if n > hi {"},
		},
	}

	got := mutatedLines(in)

	// An entry is run on its line whatever its mutator, a qualifier that
	// names another function adds nothing, and an untracked file runs whole.
	want := map[string][]lineRange{
		"clamp.go": {{Start: 5, End: 6}, {Start: 8, End: 8}},
		"new.go":   {{Start: 1, End: 4}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("mutatedLines = %v, want %v", got, want)
	}
	if lines := mutatedLines(mutationInput{Files: in.Files, Sources: in.Sources, Accepted: in.Accepted}); lines != nil {
		t.Errorf("module scope runs every line, got %v", lines)
	}
}

func TestChangedDiffNamesEachRunLineForGremlins(t *testing.T) {
	run := map[string][]lineRange{
		"clamp.go":    {{Start: 5, End: 6}, {Start: 8, End: 8}},
		"pkg/a b.go":  {{Start: 1, End: 1}},
		"deleted.go":  nil,
		"pkg/q\"x.go": {{Start: 2, End: 3}},
	}

	got := changedDiff(run)

	// gremlins counts a hunk's added lines from its new start; quoted names
	// survive spaces and quotes, and the placeholder keeps the diff non-empty,
	// which gremlins would otherwise take as "every line changed".
	want := strings.Join([]string{
		`diff --git "a/.levenshtein-no-changed-go-lines" "b/.levenshtein-no-changed-go-lines"`,
		`--- "a/.levenshtein-no-changed-go-lines"`,
		`+++ "b/.levenshtein-no-changed-go-lines"`,
		`@@ -0,0 +1,1 @@`,
		`+`,
		`diff --git "a/clamp.go" "b/clamp.go"`,
		`--- "a/clamp.go"`,
		`+++ "b/clamp.go"`,
		`@@ -0,0 +5,2 @@`,
		`+`,
		`+`,
		`@@ -0,0 +8,1 @@`,
		`+`,
		`diff --git "a/pkg/a b.go" "b/pkg/a b.go"`,
		`--- "a/pkg/a b.go"`,
		`+++ "b/pkg/a b.go"`,
		`@@ -0,0 +1,1 @@`,
		`+`,
		`diff --git "a/pkg/q\"x.go" "b/pkg/q\"x.go"`,
		`--- "a/pkg/q\"x.go"`,
		`+++ "b/pkg/q\"x.go"`,
		`@@ -0,0 +2,2 @@`,
		`+`,
		`+`,
		``,
	}, "\n")
	if got != want {
		t.Fatalf("changedDiff =\n%s\nwant\n%s", got, want)
	}
}

func TestGremlinsCommandReadsTheDiffOnlyWhenLinesAreLimited(t *testing.T) {
	limited := gremlinsCommand(10, "", nil, true)
	every := gremlinsCommand(10, "", nil, false)

	if i := slices.Index(limited, "--diff"); i < 0 || limited[i+1] != gremlinsDiffRef {
		t.Errorf("a limited run must pass --diff %s: %q", gremlinsDiffRef, limited)
	}
	if slices.Contains(every, "--diff") {
		t.Errorf("a run over every line must not pass --diff, which mutates only changed lines: %q", every)
	}
}

func TestDecideMutationCountsEveryLineWithoutARange(t *testing.T) {
	for name, lines := range map[string]map[string][]lineRange{
		"module scope":   nil,
		"untracked file": {},
	} {
		t.Run(name, func(t *testing.T) {
			in := mutationInput{Module: ".", Files: []string{"clamp.go"}, Lines: lines, Sources: sources(t, "weak", "clamp.go")}

			verdict, err := decideMutation(reported(report(t, "weak")), in)
			if err != nil {
				t.Fatal(err)
			}

			if len(verdict.Findings) != 2 || verdict.Summary.Unchanged != 0 {
				t.Fatalf("both survivors fail when the file has no ranges: %+v", verdict)
			}
		})
	}
}

func TestDecideMutationPassesADeletionOnlyChange(t *testing.T) {
	// A file whose change only removed lines has an entry with no ranges, so
	// none of its mutants run.
	in := mutationInput{Module: ".", Files: []string{"clamp.go"}, Lines: map[string][]lineRange{"clamp.go": {}}, Sources: sources(t, "weak", "clamp.go")}

	verdict, err := decideMutation(reported(skippedOn(report(t, "weak"), 5, 8)), in)
	if err != nil {
		t.Fatal(err)
	}

	if len(verdict.Findings) != 0 || verdict.Summary.Skipped != 4 || verdict.Summary.Killed != 0 {
		t.Fatalf("a deletion-only change runs nothing it did not write: %+v", verdict)
	}
}

func TestParseLinesMatchesTheSelection(t *testing.T) {
	lines, err := parseLines("", []string{"a.go"})
	if err != nil || lines != nil {
		t.Fatalf("an empty argument counts every line: %v %v", lines, err)
	}
	lines, err = parseLines(`{"a.go":[{"start":3,"end":4}]}`, []string{"a.go", "b.go"})
	if err != nil || len(lines["a.go"]) != 1 || lines["a.go"][0].End != 4 {
		t.Fatalf("valid ranges: %v %v", lines, err)
	}

	for _, raw := range []string{
		`not json`,
		`null`,
		`{"c.go":[{"start":1,"end":1}]}`,
		`{"a.go":[{"start":0,"end":1}]}`,
		`{"a.go":[{"start":5,"end":4}]}`,
	} {
		if _, err := parseLines(raw, []string{"a.go"}); err == nil {
			t.Errorf("accepted invalid lines %s", raw)
		}
	}
}

func TestDecideMutationReportsUncoveredCodeWithoutFailing(t *testing.T) {
	in := mutationInput{Module: ".", Files: []string{"clamp.go", "limits/limits.go"}, Sources: sources(t, "weak", "clamp.go", "limits/limits.go")}

	verdict, err := decideMutation(reported(report(t, "uncovered")), in)
	if err != nil {
		t.Fatal(err)
	}

	if verdict.Summary.NotCovered != 2 || len(verdict.Summary.Uncovered) != 2 || verdict.Summary.Uncovered[0].File != "limits/limits.go" {
		t.Fatalf("uncovered mutants must be listed: %+v", verdict.Summary)
	}
	for _, finding := range verdict.Findings {
		if strings.HasPrefix(finding.Location.File, "limits/") {
			t.Errorf("uncovered code must not fail the check: %+v", finding)
		}
	}
}

func TestDecideMutationSortsFindingsWhateverGremlinsOrder(t *testing.T) {
	// Gremlins lists mutations in completion order; reverse the recorded order.
	body := report(t, "weak")
	first := `{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":5,"column":7}`
	second := `{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":8,"column":7}`
	if !strings.Contains(body, first) || !strings.Contains(body, second) {
		t.Fatalf("recorded report changed shape: %s", body)
	}
	swapped := strings.NewReplacer(first, second, second, first).Replace(body)

	verdict, err := decideMutation(reported(swapped), mutationInput{Module: ".", Files: []string{"clamp.go"}, Sources: sources(t, "weak", "clamp.go")})
	if err != nil {
		t.Fatal(err)
	}

	if verdict.Findings[0].Location.Line != 5 || verdict.Findings[1].Location.Line != 8 {
		t.Fatalf("findings must be in line order: %+v", verdict.Findings)
	}
}

func TestDecideMutationNoMutantsPassesOnlyWhenGremlinsSaysSo(t *testing.T) {
	in := mutationInput{Module: ".", Files: []string{"clamp.go"}}

	verdict, err := decideMutation(mutationRun{Stdout: "Starting...\n\nNo results to report.\n"}, in)
	if err != nil || len(verdict.Findings) != 0 {
		t.Fatalf("no mutants is a pass: %+v %v", verdict, err)
	}

	if _, err := decideMutation(mutationRun{Stdout: "Starting...\n"}, in); err == nil || !strings.Contains(err.Error(), "no report") {
		t.Fatalf("a missing report without gremlins' message is an error: %v", err)
	}
}

func TestDecideMutationRejectsBrokenRuns(t *testing.T) {
	weak := report(t, "weak")
	in := mutationInput{Module: ".", Files: []string{"clamp.go"}, Sources: sources(t, "weak", "clamp.go")}
	for _, tc := range []struct {
		name string
		run  mutationRun
		want string
	}{
		{"tool error", mutationRun{ExitCode: 1, Stderr: "ERROR: not in a Go module"}, "exited 1"},
		{"threshold exit", mutationRun{ExitCode: 10, Report: weak, Reported: true}, "exited 10"},
		{"logged error", mutationRun{Report: weak, Reported: true, Stderr: "ERROR: impossible to write file"}, "ERROR"},
		{"invalid JSON", mutationRun{Report: "{", Reported: true}, "invalid gremlins report"},
		{"unselected file", mutationRun{Report: strings.Replace(weak, `"clamp.go"`, `"limits/limits.go"`, 1), Reported: true}, "not selected"},
		{"unknown status", mutationRun{Report: strings.Replace(weak, `"KILLED"`, `"ZOMBIE"`, 1), Reported: true}, "unknown status"},
		{"dry run", mutationRun{Report: strings.Replace(weak, `"KILLED"`, `"RUNNABLE"`, 1), Reported: true}, "dry run"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := decideMutation(tc.run, in)

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error mentioning %q, got %v", tc.want, err)
			}
		})
	}
}

func TestExclusionsLeaveExactlyTheSelectedFiles(t *testing.T) {
	all := []string{"a.go", "b.go", "c+d.go", "pkg/one.go", "pkg/two.go", "pkg/deep/three.go", "other/four.go", "testdata/fixture.go"}
	for _, tc := range []struct {
		name     string
		selected []string
		patterns int
	}{
		{"one root file", []string{"a.go"}, 5},
		{"a nested file", []string{"pkg/deep/three.go"}, 4},
		{"a whole directory", []string{"pkg/one.go", "pkg/two.go"}, 4},
		{"a name with regexp metacharacters", []string{"c+d.go"}, 5},
		{"everything", all, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			patterns := exclusions(all, tc.selected)

			if len(patterns) != tc.patterns {
				t.Errorf("got %d patterns, want one per directory with unselected files (%d): %q", len(patterns), tc.patterns, patterns)
			}
			var compiled []*regexp.Regexp
			for _, pattern := range patterns {
				compiled = append(compiled, regexp.MustCompile(pattern))
			}
			for _, file := range all {
				excluded := slices.ContainsFunc(compiled, func(r *regexp.Regexp) bool { return r.MatchString(file) })
				if excluded == slices.Contains(tc.selected, file) {
					t.Errorf("%s: excluded=%v, want %v", file, excluded, !excluded)
				}
			}
		})
	}
}

func TestParseAcceptedRequiresAReasonAndAKnownShape(t *testing.T) {
	entries, err := parseAccepted("", false)
	if err != nil || entries != nil {
		t.Fatalf("a missing file accepts nothing: %v %v", entries, err)
	}

	for _, tc := range []struct {
		name string
		text string
		want string
	}{
		{"wrong version", `{"version": 2, "accepted": []}`, `"version": 1`},
		{"unknown field", `{"version": 1, "accepted": [], "ignore": []}`, "unknown field"},
		{"empty reason", `{"version": 1, "accepted": [{"file": "a.go", "mutator": "ARITHMETIC_BASE", "line": "a + b", "reason": " "}]}`, "needs a reason"},
		{"missing line", `{"version": 1, "accepted": [{"file": "a.go", "mutator": "ARITHMETIC_BASE", "reason": "x"}]}`, "needs file, mutator, and line"},
		{"negative occurrence", `{"version": 1, "accepted": [{"file": "a.go", "mutator": "ARITHMETIC_BASE", "line": "a + b", "occurrence": -1, "reason": "x"}]}`, "occurrence of 1 or more"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseAccepted(tc.text, true)

			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error mentioning %q, got %v", tc.want, err)
			}
		})
	}
}

func TestValidMutationFilesKeepsArgumentsInsideTheModule(t *testing.T) {
	if err := validMutationFiles([]string{"a.go", "pkg/b.go"}); err != nil {
		t.Fatalf("valid selection rejected: %v", err)
	}

	for _, files := range [][]string{
		nil,
		{"../escape.go"},
		{"/abs.go"},
		{"pkg/../a.go"},
		{"a_test.go"},
		{"notes.md"},
		{"b.go", "a.go"},
		{"a.go", "a.go"},
	} {
		if err := validMutationFiles(files); err == nil {
			t.Errorf("accepted invalid selection %q", files)
		}
	}
}

// warmCacheReport is gremlins' report from the warm-cache reproduction: a
// module with a fast package and a package whose only test takes two seconds.
// Gremlins' coverage run replayed go test's cached result in 29ms, so every
// mutant of the slow package ran out of its 294ms limit before its test ended.
const warmCacheReport = `{"go_module":"example.com/h2","files":[` +
	`{"file_name":"fast/fast.go","mutations":[{"type":"ARITHMETIC_BASE","status":"KILLED","line":5,"column":11}]},` +
	`{"file_name":"slow/slow.go","mutations":[` +
	`{"type":"CONDITIONALS_BOUNDARY","status":"TIMED OUT","line":5,"column":7},` +
	`{"type":"CONDITIONALS_NEGATION","status":"TIMED OUT","line":5,"column":7},` +
	`{"type":"CONDITIONALS_BOUNDARY","status":"TIMED OUT","line":8,"column":7},` +
	`{"type":"CONDITIONALS_NEGATION","status":"TIMED OUT","line":8,"column":7}]}]}`

// warmCacheStdout is what gremlins printed before its mutants in that run.
const warmCacheStdout = "Starting...\nGathering coverage... go: no module dependencies to download\ndone in 29.378255ms\n"

func warmCacheInput(t *testing.T) mutationInput {
	t.Helper()
	clamp := sources(t, "weak", "clamp.go")["clamp.go"]
	return mutationInput{Module: ".", Files: []string{"fast/fast.go", "slow/slow.go"}, Sources: map[string][]string{"fast/fast.go": clamp, "slow/slow.go": clamp}}
}

func TestDecideMutationDoesNotCountTimeoutsUnderTheFloorAsCaught(t *testing.T) {
	run := mutationRun{Stdout: warmCacheStdout, Report: warmCacheReport, Reported: true, Coefficient: gremlinsTimeoutCoef}

	verdict, err := decideMutation(run, warmCacheInput(t))
	if err != nil {
		t.Fatal(err)
	}

	// Before the floor, the killed fast mutant made this a pass, and the slow
	// package's weak test was never judged.
	if !verdict.Incomplete {
		t.Fatalf("timeouts under a 294ms limit are no verdict, want incomplete: %+v", verdict)
	}
	if got := codes(verdict.Findings); !slices.Equal(got, []string{"go-mutation-timeout", "go-mutation-timeout", "go-mutation-timeout", "go-mutation-timeout"}) {
		t.Fatalf("findings = %v, want one timeout finding per slow mutant", got)
	}
	if !strings.Contains(verdict.Findings[0].Message, "293.78255ms") {
		t.Errorf("the finding should name the limit that was too short: %q", verdict.Findings[0].Message)
	}
}

func TestGremlinsGoFlagsRunTestsInsteadOfReplayingThem(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":         "module example.com/cached\n\ngo 1.21\n",
		"cached_test.go": "package cached\n\nimport \"testing\"\n\nfunc TestNothing(t *testing.T) {}\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	goTest := func(goflags string) string {
		t.Helper()
		// gremlins' own coverage command, with a fresh profile path each time.
		cmd := exec.CommandContext(t.Context(), "go", "test", "-cover", "-coverprofile", filepath.Join(t.TempDir(), "cover.out"), "./...")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOFLAGS="+goflags, "GOWORK=off", "GOTOOLCHAIN=local")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("go test: %v\n%s", err, out)
		}
		return string(out)
	}

	goTest("")
	if out := goTest(""); !strings.Contains(out, "(cached)") {
		t.Fatalf("without the flags the second run should replay the cache, so this test proves nothing: %s", out)
	}
	if out := goTest(gremlinsGoFlags); strings.Contains(out, "(cached)") {
		t.Fatalf("GOFLAGS=%s must run the tests, but go test replayed them: %s", gremlinsGoFlags, out)
	}
}

func TestCoverageTimeReadsGremlinsOutput(t *testing.T) {
	for _, tc := range []struct {
		stdout string
		want   time.Duration
		ok     bool
	}{
		{warmCacheStdout, 29378255 * time.Nanosecond, true},
		{"Starting...\nGathering coverage... done in 2.294792833s\n", 2294792833 * time.Nanosecond, true},
		{"Starting...\nGathering coverage... done in 1m3.5s\n", 63500 * time.Millisecond, true},
		{"Starting...\nNo results to report.\n", 0, false},
		{"Starting...\nGathering coverage... done in soon\n", 0, false},
	} {
		got, ok := coverageTime(tc.stdout)

		if got != tc.want || ok != tc.ok {
			t.Errorf("coverageTime(%q) = %s, %v; want %s, %v", tc.stdout, got, ok, tc.want, tc.ok)
		}
	}
}

func TestLongerCoefficientLiftsAShortLimitAboveTheFloor(t *testing.T) {
	warm := mutationRun{Stdout: warmCacheStdout, Report: warmCacheReport, Reported: true, Coefficient: gremlinsTimeoutCoef}

	coefficient, retry := longerCoefficient(warm)

	if !retry {
		t.Fatal("a timeout under a 294ms limit must be run again")
	}
	if limit := 29378255 * time.Nanosecond * time.Duration(coefficient); limit < 2*mutantTimeFloor {
		t.Errorf("coefficient %d gives a %s limit, want at least twice the %s floor", coefficient, limit, mutantTimeFloor)
	}
	if got := gremlinsCommand(coefficient, "", nil, false); !slices.Contains(got, strconv.Itoa(coefficient)) {
		t.Errorf("the command %q must pass coefficient %d", got, coefficient)
	}

	for name, run := range map[string]mutationRun{
		"trusted limit": {Stdout: "Gathering coverage... done in 1s\n", Report: warmCacheReport, Reported: true, Coefficient: gremlinsTimeoutCoef},
		"no timeouts":   {Stdout: warmCacheStdout, Report: report(t, "weak"), Reported: true, Coefficient: gremlinsTimeoutCoef},
		"failed run":    {ExitCode: 1, Stdout: warmCacheStdout, Report: warmCacheReport, Reported: true, Coefficient: gremlinsTimeoutCoef},
	} {
		if _, retry := longerCoefficient(run); retry {
			t.Errorf("%s: must not run gremlins again", name)
		}
	}
}

func TestDecideMutationWarnsWhenMostOfAPackageTimedOut(t *testing.T) {
	// The same run with a trusted limit: the slow package's timeouts count as
	// caught, but four of four is worth a person's look.
	run := mutationRun{Stdout: "Gathering coverage... done in 2s\n", Report: warmCacheReport, Reported: true, Coefficient: gremlinsTimeoutCoef}

	verdict, err := decideMutation(run, warmCacheInput(t))
	if err != nil {
		t.Fatal(err)
	}

	if verdict.Incomplete || len(verdict.Findings) != 0 {
		t.Fatalf("timeouts under a trusted limit are caught: %+v", verdict)
	}
	if len(verdict.Summary.Warnings) != 1 || !strings.Contains(verdict.Summary.Warnings[0], "4 of 4 covered mutants in slow timed out under a 20s limit") {
		t.Errorf("want one warning for the slow package, got %q", verdict.Summary.Warnings)
	}

	// One timeout among killed mutants is an ordinary hang.
	verdict, err = decideMutation(reported(report(t, "timedout")), mutationInput{Module: ".", Files: []string{"clamp.go"}, Sources: sources(t, "weak", "clamp.go")})
	if err != nil {
		t.Fatal(err)
	}
	if len(verdict.Summary.Warnings) != 0 {
		t.Errorf("one timeout in four is not worth a warning: %q", verdict.Summary.Warnings)
	}
}

// twice has the same condition in two functions, on lines 4 and 11.
const twice = `package limits

func Low(n int) int {
	if n > 10 {
		return 10
	}
	return n
}

func (l *Limiter[T]) High(n int) int {
	if n > 10 {
		return 10
	}
	return n
}
`

const twiceReport = `{"go_module":"example.com/limits","files":[{"file_name":"limits.go","mutations":[` +
	`{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":4,"column":7},` +
	`{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":11,"column":7}]}]}`

func TestDecideMutationRejectsAnEntryThatMatchesSurvivorsOnSeveralLines(t *testing.T) {
	text := "{\"version\": 1, \"accepted\": [\n  {\"file\": \"limits.go\", \"mutator\": \"CONDITIONALS_BOUNDARY\", \"line\": \"if n > 10 {\", \"reason\": \"equivalent in Low\"}\n]}\n"
	entries, err := parseAccepted(text, true)
	if err != nil {
		t.Fatal(err)
	}
	in := mutationInput{Module: ".", Files: []string{"limits.go"}, Sources: map[string][]string{"limits.go": strings.Split(twice, "\n")}, Accepted: entries, AcceptedPath: "accepted.json", AcceptedText: text}

	verdict, err := decideMutation(reported(twiceReport), in)
	if err != nil {
		t.Fatal(err)
	}

	// One entry must not silence the same text wherever it appears in the file.
	if got := codes(verdict.Findings); !slices.Equal(got, []string{"go-mutation", "go-mutation", "go-mutation-ambiguous"}) {
		t.Fatalf("findings = %v, want both survivors and the ambiguous entry", got)
	}
	ambiguous := verdict.Findings[2]
	if ambiguous.Location.File != "accepted.json" || ambiguous.Location.Line != 2 || !strings.Contains(ambiguous.Message, "4, 11") {
		t.Errorf("ambiguous finding = %+v, want it at the entry naming lines 4 and 11", ambiguous)
	}
	if verdict.Summary.Accepted != 0 {
		t.Errorf("an ambiguous entry accepts nothing: %+v", verdict.Summary)
	}
}

func TestDecideMutationEntryQualifiersPickOneLine(t *testing.T) {
	for _, tc := range []struct {
		name     string
		entry    string
		accepted int
		failed   int
	}{
		{"function", `"function": "Low"`, 4, 11},
		{"method on a generic type", `"function": "Limiter.High"`, 11, 4},
		{"occurrence", `"occurrence": 2`, 11, 4},
		{"occurrence within a function", `"function": "Low", "occurrence": 1`, 4, 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := `{"version": 1, "accepted": [{"file": "limits.go", "mutator": "CONDITIONALS_BOUNDARY", "line": "if n > 10 {", ` + tc.entry + `, "reason": "equivalent here"}]}`
			entries, err := parseAccepted(text, true)
			if err != nil {
				t.Fatal(err)
			}
			in := mutationInput{Module: ".", Files: []string{"limits.go"}, Sources: map[string][]string{"limits.go": strings.Split(twice, "\n")}, Accepted: entries}

			verdict, err := decideMutation(reported(twiceReport), in)
			if err != nil {
				t.Fatal(err)
			}

			if len(verdict.Findings) != 1 || verdict.Findings[0].Code != "go-mutation" || verdict.Findings[0].Location.Line != tc.failed {
				t.Fatalf("want only the line-%d survivor to fail, got %+v", tc.failed, verdict.Findings)
			}
			if verdict.Summary.Accepted != 1 {
				t.Errorf("want the line-%d survivor accepted: %+v", tc.accepted, verdict.Summary)
			}
		})
	}
}

func TestDecideMutationFlagsAnEntryWhoseMutantIsNowCaught(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "mutation", "accepted", ".levenshtein", "mutation-accepted.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	entries, err := parseAccepted(text, true)
	if err != nil {
		t.Fatal(err)
	}
	in := mutationInput{Module: ".", Files: []string{"clamp.go"}, Sources: sources(t, "accepted", "clamp.go"), Accepted: entries, AcceptedPath: ".levenshtein/mutation-accepted.json", AcceptedText: text}
	// A new test kills the line-5 survivor, and the line-8 mutant now hangs.
	body := strings.NewReplacer(
		`"CONDITIONALS_BOUNDARY","status":"LIVED","line":5`, `"CONDITIONALS_BOUNDARY","status":"KILLED","line":5`,
		`"CONDITIONALS_BOUNDARY","status":"LIVED","line":8`, `"CONDITIONALS_BOUNDARY","status":"TIMED OUT","line":8`,
	).Replace(report(t, "accepted"))

	verdict, err := decideMutation(reported(body), in)
	if err != nil {
		t.Fatal(err)
	}

	// Kept, either entry would accept its survivor again if that test were
	// deleted.
	if got := codes(verdict.Findings); !slices.Equal(got, []string{"go-mutation-stale", "go-mutation-stale"}) {
		t.Fatalf("findings = %v, want both entries stale", got)
	}
	for i, line := range []int{7, 13} {
		finding := verdict.Findings[i]
		if finding.Location.Line != line || !strings.Contains(finding.Message, "now caught") {
			t.Errorf("finding %d = %+v, want a now-caught stale entry at line %d", i, finding, line)
		}
	}
}

func TestDecideMutationKeepsAnEntryWhoseMutantIsNotCovered(t *testing.T) {
	entries := []acceptedEntry{{File: "clamp.go", Mutator: "CONDITIONALS_BOUNDARY", Line: "if n < lo {", Reason: "equivalent"}}
	in := mutationInput{Module: ".", Files: []string{"clamp.go"}, Sources: sources(t, "accepted", "clamp.go"), Accepted: entries}
	body := strings.ReplaceAll(report(t, "accepted"), `"LIVED"`, `"NOT COVERED"`)

	verdict, err := decideMutation(reported(body), in)
	if err != nil {
		t.Fatal(err)
	}

	// No test reaches the line, which says nothing about whether one could
	// kill the mutant.
	if len(verdict.Findings) != 0 {
		t.Fatalf("an entry whose mutant is not covered is not stale: %+v", verdict.Findings)
	}
}

func TestDecideMutationPointsAStaleEntryAtItsOwnLine(t *testing.T) {
	// Both entries quote the same text; only the second, for clamp.go, is judged.
	text := "{\"version\": 1, \"accepted\": [\n  {\"file\": \"other.go\", \"mutator\": \"CONDITIONALS_BOUNDARY\", \"line\": \"if n <= 0 {\", \"reason\": \"not mutated\"},\n  {\"file\": \"clamp.go\", \"mutator\": \"CONDITIONALS_BOUNDARY\", \"line\": \"if n <= 0 {\", \"reason\": \"old\"}\n]}\n"
	entries, err := parseAccepted(text, true)
	if err != nil {
		t.Fatal(err)
	}
	in := mutationInput{Module: ".", Files: []string{"clamp.go"}, Sources: sources(t, "accepted", "clamp.go"), Accepted: entries, AcceptedPath: "accepted.json", AcceptedText: text}

	verdict, err := decideMutation(reported(report(t, "accepted")), in)
	if err != nil {
		t.Fatal(err)
	}

	stale := slices.IndexFunc(verdict.Findings, func(d diagnostic) bool { return d.Code == "go-mutation-stale" })
	if stale < 0 || verdict.Findings[stale].Location.Line != 3 {
		t.Fatalf("want the clamp.go entry on line 3 flagged, got %+v", verdict.Findings)
	}
}
