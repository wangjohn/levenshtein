package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
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

func reported(body string) mutationRun {
	return mutationRun{Report: body, Reported: true, Stderr: "go: no module dependencies to download\n"}
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

func TestDecideMutationIsIncompleteWhenNothingWasKilled(t *testing.T) {
	// Every mutant that should have been killed timed out instead, which points
	// at the machine rather than the tests.
	body := strings.ReplaceAll(report(t, "weak"), `"KILLED"`, `"TIMED OUT"`)
	in := mutationInput{Module: ".", Files: []string{"clamp.go"}, Sources: sources(t, "weak", "clamp.go")}

	verdict, err := decideMutation(reported(body), in)
	if err != nil {
		t.Fatal(err)
	}

	if !verdict.Incomplete {
		t.Fatalf("no killed mutants and some timeouts must be incomplete: %+v", verdict)
	}
	if got := codes(verdict.Findings); !slices.Equal(got, []string{"go-mutation", "go-mutation", "go-mutation-timeout", "go-mutation-timeout"}) {
		t.Errorf("findings = %v, want both survivors and both timeouts", got)
	}
}

func TestDecideMutationFailsOnlyOnChangedLines(t *testing.T) {
	in := mutationInput{
		Module:  ".",
		Files:   []string{"clamp.go"},
		Lines:   map[string][]lineRange{"clamp.go": {{Start: 4, End: 6}}},
		Sources: sources(t, "weak", "clamp.go"),
	}

	verdict, err := decideMutation(reported(report(t, "weak")), in)
	if err != nil {
		t.Fatal(err)
	}

	if len(verdict.Findings) != 1 || verdict.Findings[0].Location.Line != 5 {
		t.Fatalf("only the survivor on changed line 5 fails: %+v", verdict.Findings)
	}
	if verdict.Summary.Lived != 1 || verdict.Summary.Unchanged != 1 || verdict.Summary.UnchangedList[0].Line != 8 {
		t.Errorf("the line-8 survivor must be listed as unchanged: %+v", verdict.Summary)
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
	// every survivor in it is inherited.
	in := mutationInput{Module: ".", Files: []string{"clamp.go"}, Lines: map[string][]lineRange{"clamp.go": {}}, Sources: sources(t, "weak", "clamp.go")}

	verdict, err := decideMutation(reported(report(t, "weak")), in)
	if err != nil {
		t.Fatal(err)
	}

	if len(verdict.Findings) != 0 || verdict.Summary.Unchanged != 2 {
		t.Fatalf("a deletion-only change fails on nothing it did not write: %+v", verdict)
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
