package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"maps"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"dagger/levenshtein/internal/dagger"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

// Gremlins settings that decide results. A fixed worker count keeps load, and
// so the per-mutant time limits, the same from one machine to the next. Each
// limit is the coverage run's duration times the coefficient.
const (
	gremlinsWorkers     = "2"
	gremlinsTimeoutCoef = 10
	gremlinsReportDir   = "/report"
	gremlinsReportPath  = gremlinsReportDir + "/out.json"
	gremlinsNoResults   = "No results to report."

	// gremlinsGoFlags makes gremlins' coverage run and every mutant's test run
	// execute the tests. The build cache persists between runs, and a coverage
	// run that replays go test's cached result takes milliseconds, which would
	// set limits too short for the tests to finish.
	gremlinsGoFlags = "-count=1"

	// mutantTimeFloor is the shortest per-mutant limit whose timeouts count as
	// caught. Before its tests start, a mutant's run recompiles the package and
	// links a test binary, which takes seconds on a shared CI runner even with
	// a warm build cache; a shorter limit can end a run that would not hang.
	mutantTimeFloor = 10 * time.Second

	// gremlinsAttempts bounds how often gremlins runs with a raised
	// coefficient when a limit fell below the floor and a mutant timed out.
	gremlinsAttempts = 3

	// minTimeoutsForIncomplete is how many covered mutants have to time out,
	// with none killed or surviving, before the run is blamed on the machine.
	minTimeoutsForIncomplete = 3

	// minTimeoutsForWarning is how many of a package's covered mutants have to
	// time out, and be at least half of them, before the summary warns that
	// its tests may be slower than the limit rather than hung by the mutants.
	minTimeoutsForWarning = 2

	// defaultAcceptedPath matches the CLI default and the +default below.
	defaultAcceptedPath = ".levenshtein/mutation-accepted.json"
)

// mutationStatus is a gremlins mutant status as its JSON report spells it.
type mutationStatus string

const (
	mutationKilled     mutationStatus = "KILLED"
	mutationLived      mutationStatus = "LIVED"
	mutationNotCovered mutationStatus = "NOT COVERED"
	mutationTimedOut   mutationStatus = "TIMED OUT"
	mutationNotViable  mutationStatus = "NOT VIABLE"
	mutationSkipped    mutationStatus = "SKIPPED"
	mutationRunnable   mutationStatus = "RUNNABLE"
)

// gremlinsReport mirrors the fields of gremlins' --output file that the
// verdict reads. Counts are recomputed from the mutations themselves.
type gremlinsReport struct {
	GoModule string         `json:"go_module"`
	Files    []gremlinsFile `json:"files"`
}

type gremlinsFile struct {
	FileName  string             `json:"file_name"`
	Mutations []gremlinsMutation `json:"mutations"`
}

type gremlinsMutation struct {
	Type   string         `json:"type"`
	Status mutationStatus `json:"status"`
	Line   int            `json:"line"`
	Column int            `json:"column"`
}

// acceptedFile is the checked-in list of survivors no test can kill.
type acceptedFile struct {
	Version  int             `json:"version"`
	Accepted []acceptedEntry `json:"accepted"`
}

// acceptedEntry matches a surviving mutant by the text of its line rather than
// the line number, so edits elsewhere in the file do not break the match.
// Function and Occurrence narrow an entry whose text appears on more than one
// line: Function names the enclosing function, as Name or Type.Method, and
// Occurrence counts lines with that text from the top of the file, or of the
// function when Function is set, starting at 1.
type acceptedEntry struct {
	File       string `json:"file"`
	Mutator    string `json:"mutator"`
	Line       string `json:"line"`
	Function   string `json:"function,omitempty"`
	Occurrence int    `json:"occurrence,omitempty"`
	Reason     string `json:"reason"`
}

// mutationRun is what one gremlins invocation produced, and the coefficient it
// ran with, which with the coverage time in Stdout gives each mutant's limit.
type mutationRun struct {
	ExitCode    int
	Stdout      string
	Stderr      string
	Report      string
	Reported    bool
	Coefficient int
}

// mutationSummary is returned on every completed run, so a person sees the
// counts and uncovered lines whether or not the check failed.
type mutationSummary struct {
	Killed          int              `json:"killed"`
	Lived           int              `json:"lived"`
	Unchanged       int              `json:"unchanged_survivors"`
	Accepted        int              `json:"accepted"`
	NotCovered      int              `json:"not_covered"`
	TimedOut        int              `json:"timed_out"`
	NotViable       int              `json:"not_viable"`
	Skipped         int              `json:"skipped"`
	Uncovered       []mutationMutant `json:"uncovered,omitempty"`
	UnchangedList   []mutationMutant `json:"unchanged,omitempty"`
	TimedOutMutants []mutationMutant `json:"timed_out_mutants,omitempty"`
	Warnings        []string         `json:"warnings,omitempty"`
	Files           []string         `json:"files"`
}

// lineRange is an inclusive run of changed lines, as the CLI sends them.
type lineRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type mutationMutant struct {
	File    string `json:"file"`
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Mutator string `json:"mutator"`
}

// mutationVerdict is the outcome of one run: findings to report, whether the
// run could not be judged at all, and the counts.
type mutationVerdict struct {
	Findings   []diagnostic
	Incomplete bool
	Summary    mutationSummary
}

// mutationInput is everything the verdict depends on besides gremlins itself.
type mutationInput struct {
	Module       string
	Files        []string
	Lines        map[string][]lineRange
	Sources      map[string][]string
	Accepted     []acceptedEntry
	AcceptedPath string
	AcceptedText string
}

// parseAccepted reads the accepted-survivors file. A repository without one
// accepts nothing.
func parseAccepted(data string, present bool) ([]acceptedEntry, error) {
	if !present {
		return nil, nil
	}

	decoder := json.NewDecoder(strings.NewReader(data))
	decoder.DisallowUnknownFields()
	var file acceptedFile
	if err := decoder.Decode(&file); err != nil {
		return nil, fmt.Errorf("accepted survivors file: %w", err)
	}
	if file.Version != 1 {
		return nil, fmt.Errorf("accepted survivors file needs \"version\": 1, not %d", file.Version)
	}
	for i, entry := range file.Accepted {
		if entry.File == "" || entry.Mutator == "" || strings.TrimSpace(entry.Line) == "" {
			return nil, fmt.Errorf("accepted survivor #%d needs file, mutator, and line", i+1)
		}
		if strings.TrimSpace(entry.Reason) == "" {
			return nil, fmt.Errorf("accepted survivor #%d (%s, %s) needs a reason", i+1, entry.File, entry.Mutator)
		}
		if entry.Occurrence < 0 {
			return nil, fmt.Errorf("accepted survivor #%d (%s, %s) needs an occurrence of 1 or more", i+1, entry.File, entry.Mutator)
		}
	}
	return file.Accepted, nil
}

// exclusions returns one gremlins -E pattern per directory, so the number of
// patterns grows with directories rather than files. Gremlins walks every
// directory under the module root and matches slash-separated, module-relative
// paths, so every file outside selected has to be named or covered.
func exclusions(all, selected []string) []string {
	chosen := map[string]bool{}
	for _, file := range selected {
		chosen[file] = true
	}
	others := map[string][]string{}
	touched := map[string]bool{}
	for _, file := range all {
		dir := path.Dir(file)
		if chosen[file] {
			touched[dir] = true
			continue
		}
		others[dir] = append(others[dir], path.Base(file))
	}

	var patterns []string
	for _, dir := range slices.Sorted(maps.Keys(others)) {
		prefix := ""
		if dir != "." {
			prefix = regexp.QuoteMeta(dir + "/")
		}
		if !touched[dir] {
			patterns = append(patterns, "^"+prefix+`[^/]+\.go$`)
			continue
		}
		names := others[dir]
		slices.Sort(names)
		quoted := make([]string, len(names))
		for i, name := range names {
			quoted[i] = regexp.QuoteMeta(name)
		}
		patterns = append(patterns, "^"+prefix+"(?:"+strings.Join(quoted, "|")+")$")
	}
	return patterns
}

// decideMutation turns one gremlins run into a verdict. It is the only place
// outcomes are decided, and it never relies on gremlins' own thresholds: they
// are ignored as flags and off by one in configuration.
//
// A survivor fails the check only on a line the change wrote, so a pull request
// is not failed for gaps it inherited; other survivors are listed in the
// summary. A timed-out mutant counts as caught, the way PIT and Stryker count
// it: a mutation that makes the code hang is one the tests noticed. That holds
// only for a limit of at least mutantTimeFloor; a timeout under a shorter one,
// or under a limit the run did not report, makes the run incomplete. A run is
// also incomplete when every covered mutant timed out, and there were enough of
// them that deliberate hangs are an unlikely explanation; that points at the
// machine rather than the code.
func decideMutation(run mutationRun, in mutationInput) (mutationVerdict, error) {
	summary := mutationSummary{Files: in.Files}
	if run.ExitCode != 0 || strings.Contains(run.Stderr, "ERROR:") {
		return mutationVerdict{}, fmt.Errorf("gremlins exited %d: %s", run.ExitCode, strings.TrimSpace(run.Stdout+"\n"+run.Stderr))
	}
	if !run.Reported {
		// Gremlins writes no report when it found nothing to mutate, and says so.
		if strings.Contains(run.Stdout, gremlinsNoResults) {
			return mutationVerdict{Summary: summary}, nil
		}
		return mutationVerdict{}, fmt.Errorf("gremlins produced no report: %s", strings.TrimSpace(run.Stdout+"\n"+run.Stderr))
	}

	var report gremlinsReport
	if err := json.Unmarshal([]byte(run.Report), &report); err != nil {
		return mutationVerdict{}, fmt.Errorf("invalid gremlins report: %w", err)
	}

	selected := map[string]bool{}
	for _, file := range in.Files {
		selected[file] = true
	}
	var lived, timedOut []mutationMutant
	mutated := map[string]bool{}
	covered := map[string]int{}
	timedOutIn := map[string]int{}
	for _, file := range report.Files {
		if !selected[file.FileName] {
			return mutationVerdict{}, fmt.Errorf("gremlins mutated %q, which was not selected", file.FileName)
		}
		mutated[file.FileName] = true
		for _, m := range file.Mutations {
			mutant := mutationMutant{File: file.FileName, Line: m.Line, Column: m.Column, Mutator: m.Type}
			switch m.Status {
			case mutationKilled:
				summary.Killed++
				covered[path.Dir(mutant.File)]++
			case mutationLived:
				lived = append(lived, mutant)
				covered[path.Dir(mutant.File)]++
			case mutationNotCovered:
				summary.NotCovered++
				summary.Uncovered = append(summary.Uncovered, mutant)
			case mutationTimedOut:
				summary.TimedOut++
				timedOut = append(timedOut, mutant)
				covered[path.Dir(mutant.File)]++
				timedOutIn[path.Dir(mutant.File)]++
				summary.TimedOutMutants = append(summary.TimedOutMutants, mutant)
			case mutationNotViable:
				summary.NotViable++
			case mutationSkipped:
				summary.Skipped++
			case mutationRunnable:
				return mutationVerdict{}, fmt.Errorf("gremlins reported %s mutants, which only a dry run produces", m.Status)
			default:
				return mutationVerdict{}, fmt.Errorf("gremlins reported unknown status %q at %s:%d", m.Status, file.FileName, m.Line)
			}
		}
	}

	// Every entry is matched against the whole report first: one that fits
	// survivors on more than one line accepts none of them, and one whose
	// mutants are all caught no longer describes a survivor.
	limit, measured := run.limit()
	trusted := measured && limit >= mutantTimeFloor
	index := newSourceIndex(in.Sources)
	matches := make([]entryMatch, len(in.Accepted))
	for i, entry := range in.Accepted {
		matches[i] = matchEntry(report, index, entry, trusted)
	}

	var findings []diagnostic
	for _, mutant := range sortedMutants(lived) {
		text := sourceLine(in.Sources, mutant)
		if accepts(in.Accepted, matches, index, mutant) {
			summary.Accepted++
			continue
		}
		if !onChangedLine(in.Lines, mutant) {
			summary.Unchanged++
			summary.UnchangedList = append(summary.UnchangedList, mutant)
			continue
		}
		summary.Lived++
		findings = append(findings, diagnostic{
			Code:     "go-mutation",
			Message:  fmt.Sprintf("%s mutant survived: no test fails when this line is mutated: %s", mutant.Mutator, text),
			Location: location{File: moduleFile(in.Module, mutant.File), Line: mutant.Line, Column: mutant.Column},
		})
	}
	for i, entry := range in.Accepted {
		if !mutated[entry.File] {
			continue // Entries for files this run did not mutate are not judged.
		}
		match := matches[i]
		described := fmt.Sprintf("accepted survivor for %s (%s, %q)", entry.File, entry.Mutator, entry.Line)
		code := "go-mutation-stale"
		var message string
		switch {
		case len(match.Survivors) == 1, len(match.Survivors) == 0 && match.Pending:
			continue
		case len(match.Survivors) > 1:
			code = "go-mutation-ambiguous"
			message = fmt.Sprintf("%s matches survivors on lines %s; add \"function\" or \"occurrence\" to name one", described, joinLines(match.Survivors))
		case match.Caught:
			message = described + " is now caught by a test; remove it"
		default:
			message = described + " no longer matches a mutant; remove it"
		}
		findings = append(findings, diagnostic{
			Code:     code,
			Message:  message,
			Location: location{File: in.AcceptedPath, Line: entryLine(in.AcceptedText, in.Accepted, i)},
		})
	}

	// Hangs are deterministic, so a few mutants that all hang is a result, not
	// an environment failure; incomplete would fail that change on every run,
	// with nothing the accepted file could clear. A timeout under a limit too
	// short to trust is different: the tests may never have started.
	survivors := summary.Lived + summary.Unchanged + summary.Accepted
	short := summary.TimedOut > 0 && !trusted
	incomplete := short || summary.TimedOut >= minTimeoutsForIncomplete && summary.Killed == 0 && survivors == 0
	if incomplete {
		reason := "like every other covered mutant"
		if short {
			reason = "under a limit gremlins did not report"
			if measured {
				reason = fmt.Sprintf("under a %s limit, shorter than the %s floor", limit, mutantTimeFloor)
			}
		}
		for _, mutant := range sortedMutants(timedOut) {
			findings = append(findings, diagnostic{
				Code:     "go-mutation-timeout",
				Message:  fmt.Sprintf("%s mutant timed out %s, so the run gave no verdict: %s", mutant.Mutator, reason, sourceLine(in.Sources, mutant)),
				Location: location{File: moduleFile(in.Module, mutant.File), Line: mutant.Line, Column: mutant.Column},
			})
		}
	} else {
		summary.Warnings = timeoutWarnings(covered, timedOutIn, limit)
	}
	summary.Uncovered = sortedMutants(summary.Uncovered)
	summary.UnchangedList = sortedMutants(summary.UnchangedList)
	summary.TimedOutMutants = sortedMutants(summary.TimedOutMutants)
	return mutationVerdict{Findings: findings, Incomplete: incomplete, Summary: summary}, nil
}

// timeoutWarnings names each package where timeouts were at least half of the
// covered mutants. They still count as caught, but so many hangs more often
// mean tests slower than the limit than mutations that all loop forever.
func timeoutWarnings(covered, timedOut map[string]int, limit time.Duration) []string {
	var warnings []string
	for _, dir := range slices.Sorted(maps.Keys(timedOut)) {
		n := timedOut[dir]
		if n < minTimeoutsForWarning || 2*n < covered[dir] {
			continue
		}
		warnings = append(warnings, fmt.Sprintf("%d of %d covered mutants in %s timed out under a %s limit; they count as caught, so check that its tests finish well within that limit", n, covered[dir], dir, limit))
	}
	return warnings
}

// coverageDone finds the duration gremlins prints when its coverage run ends;
// the go command's own output can come between the two parts.
var coverageDone = regexp.MustCompile(`Gathering coverage\.\.\.[\s\S]*?\bdone in (\S+)`)

// coverageTime reads how long gremlins' coverage run took from its output.
func coverageTime(stdout string) (time.Duration, bool) {
	match := coverageDone.FindStringSubmatch(stdout)
	if match == nil {
		return 0, false
	}
	elapsed, err := time.ParseDuration(match[1])
	return elapsed, err == nil
}

// limit is the time each mutant's tests had: gremlins multiplies its coverage
// time by the coefficient.
func (r mutationRun) limit() (time.Duration, bool) {
	elapsed, ok := coverageTime(r.Stdout)
	if !ok || r.Coefficient < 1 {
		return 0, false
	}
	return elapsed * time.Duration(r.Coefficient), true
}

// longerCoefficient returns the coefficient to run gremlins again with when
// this run timed out a mutant under a limit below mutantTimeFloor. It aims at
// twice the floor, so a rerun whose coverage is somewhat faster still clears
// it; the higher limit costs time only for mutants that do hang.
func longerCoefficient(run mutationRun) (int, bool) {
	var report gremlinsReport
	if run.ExitCode != 0 || !run.Reported || json.Unmarshal([]byte(run.Report), &report) != nil {
		return 0, false
	}
	timedOut := slices.ContainsFunc(report.Files, func(file gremlinsFile) bool {
		return slices.ContainsFunc(file.Mutations, func(m gremlinsMutation) bool { return m.Status == mutationTimedOut })
	})
	elapsed, measured := coverageTime(run.Stdout)
	if limit, _ := run.limit(); !timedOut || !measured || limit >= mutantTimeFloor {
		return 0, false
	}
	elapsed = max(elapsed, time.Millisecond)
	return int((2*mutantTimeFloor + elapsed - 1) / elapsed), true
}

func joinLines(lines []int) string {
	text := make([]string, len(lines))
	for i, line := range lines {
		text[i] = strconv.Itoa(line)
	}
	return strings.Join(text, ", ")
}

// onChangedLine reports whether a mutant sits on a line the change wrote. Nil
// lines, as in module scope, count every line, and so does a file without an
// entry, such as an untracked one whose every line is new.
func onChangedLine(lines map[string][]lineRange, mutant mutationMutant) bool {
	if lines == nil {
		return true
	}
	ranges, ok := lines[mutant.File]
	if !ok {
		return true
	}
	return slices.ContainsFunc(ranges, func(r lineRange) bool { return r.Start <= mutant.Line && mutant.Line <= r.End })
}

// parseLines reads the CLI's changed-line map. An empty argument makes every
// line count; a key the selection does not name is an error, so a mismatch
// between the two cannot silently widen or narrow the check.
func parseLines(raw string, files []string) (map[string][]lineRange, error) {
	if raw == "" {
		return nil, nil
	}
	var lines map[string][]lineRange
	if err := json.Unmarshal([]byte(raw), &lines); err != nil {
		return nil, fmt.Errorf("invalid go-mutation lines: %w", err)
	}
	if lines == nil {
		return nil, fmt.Errorf("go-mutation lines must be an object, or empty to count every line")
	}
	for file, ranges := range lines {
		if !slices.Contains(files, file) {
			return nil, fmt.Errorf("go-mutation lines name %q, which is not a selected file", file)
		}
		for _, r := range ranges {
			if r.Start < 1 || r.End < r.Start {
				return nil, fmt.Errorf("invalid changed-line range %d-%d for %q", r.Start, r.End, file)
			}
		}
	}
	return lines, nil
}

// sortedMutants orders mutants by position, because gremlins' own output order
// changes between runs.
func sortedMutants(mutants []mutationMutant) []mutationMutant {
	slices.SortFunc(mutants, func(a, b mutationMutant) int {
		return cmp.Or(strings.Compare(a.File, b.File), cmp.Compare(a.Line, b.Line), cmp.Compare(a.Column, b.Column), strings.Compare(a.Mutator, b.Mutator))
	})
	return mutants
}

func sourceLine(sources map[string][]string, mutant mutationMutant) string {
	lines := sources[mutant.File]
	if mutant.Line < 1 || mutant.Line > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[mutant.Line-1])
}

// entryMatch is what one accepted entry fits in a run: the distinct lines of
// the survivors it matches, and whether it matches mutants a test caught or
// mutants no test judged.
type entryMatch struct {
	Survivors []int
	Caught    bool
	Pending   bool
}

// matchEntry matches an entry against every mutant of its file. A timeout
// counts as caught only under a trusted limit.
func matchEntry(report gremlinsReport, index sourceIndex, entry acceptedEntry, trusted bool) entryMatch {
	var survivors []int
	caught := false
	pending := false
	for _, file := range report.Files {
		if file.FileName != entry.File {
			continue
		}
		for _, m := range file.Mutations {
			if !index.matches(entry, mutationMutant{File: file.FileName, Line: m.Line, Column: m.Column, Mutator: m.Type}) {
				continue
			}
			switch m.Status {
			case mutationLived:
				survivors = append(survivors, m.Line)
			case mutationKilled:
				caught = true
			case mutationTimedOut:
				caught = caught || trusted
				pending = pending || !trusted
			case mutationNotCovered, mutationNotViable, mutationSkipped, mutationRunnable:
				pending = true
			}
		}
	}
	slices.Sort(survivors)
	return entryMatch{Survivors: slices.Compact(survivors), Caught: caught, Pending: pending}
}

// accepts reports whether an entry that fits survivors on a single line
// matches this one.
func accepts(entries []acceptedEntry, matches []entryMatch, index sourceIndex, mutant mutationMutant) bool {
	for i, entry := range entries {
		if len(matches[i].Survivors) == 1 && index.matches(entry, mutant) {
			return true
		}
	}
	return false
}

// sourceIndex holds what an accepted entry matches on besides the mutator:
// each line's trimmed text and the function that encloses it.
type sourceIndex struct {
	sources   map[string][]string
	functions map[string][]string
}

func newSourceIndex(sources map[string][]string) sourceIndex {
	functions := map[string][]string{}
	for file, lines := range sources {
		functions[file] = enclosingFunctions(file, lines)
	}
	return sourceIndex{sources: sources, functions: functions}
}

func (s sourceIndex) text(file string, line int) string {
	return sourceLine(s.sources, mutationMutant{File: file, Line: line})
}

func (s sourceIndex) function(file string, line int) string {
	names := s.functions[file]
	if line < 1 || line > len(names) {
		return ""
	}
	return names[line-1]
}

// occurrence counts the lines up to and including line that have its text,
// only within its function when scoped.
func (s sourceIndex) occurrence(file string, line int, scoped bool) int {
	text := s.text(file, line)
	function := s.function(file, line)
	count := 0
	for other := 1; other <= line; other++ {
		if s.text(file, other) == text && (!scoped || s.function(file, other) == function) {
			count++
		}
	}
	return count
}

func (s sourceIndex) matches(entry acceptedEntry, mutant mutationMutant) bool {
	if entry.File != mutant.File || entry.Mutator != mutant.Mutator || strings.TrimSpace(entry.Line) != s.text(mutant.File, mutant.Line) {
		return false
	}
	if entry.Function != "" && s.function(mutant.File, mutant.Line) != entry.Function {
		return false
	}
	return entry.Occurrence == 0 || s.occurrence(mutant.File, mutant.Line, entry.Function != "") == entry.Occurrence
}

// enclosingFunctions names the function around each line of a file, as Name or
// Type.Method, or "" outside any function. A file that does not parse names
// none, so an entry that needs a function matches nothing in it.
func enclosingFunctions(file string, lines []string) []string {
	names := make([]string, len(lines))
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, strings.Join(lines, "\n"), parser.SkipObjectResolution)
	if err != nil {
		return names
	}
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := functionName(fn)
		for line := fset.Position(fn.Pos()).Line; line <= fset.Position(fn.End()).Line && line <= len(names); line++ {
			names[line-1] = name
		}
	}
	return names
}

func functionName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	receiver := fn.Recv.List[0].Type
	if star, ok := receiver.(*ast.StarExpr); ok {
		receiver = star.X
	}
	switch generic := receiver.(type) {
	case *ast.IndexExpr:
		receiver = generic.X
	case *ast.IndexListExpr:
		receiver = generic.X
	}
	if ident, ok := receiver.(*ast.Ident); ok {
		return ident.Name + "." + fn.Name.Name
	}
	return fn.Name.Name
}

// entryLine finds the line of the accepted file that holds an entry, so a
// finding points at the entry to change. Entries can share their text, so it
// skips one matching line for each earlier entry with the same text.
func entryLine(text string, entries []acceptedEntry, i int) int {
	// Encode the way a person writes the file: json.Marshal would escape the
	// < and > that comparisons are full of.
	var encoded strings.Builder
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(entries[i].Line) // A string always encodes.
	quoted := strings.TrimSpace(encoded.String())
	skip := 0
	for _, earlier := range entries[:i] {
		if earlier.Line == entries[i].Line {
			skip++
		}
	}
	for n, candidate := range strings.Split(text, "\n") {
		if !strings.Contains(candidate, quoted) {
			continue
		}
		if skip == 0 {
			return n + 1
		}
		skip--
	}
	return 1
}

func moduleFile(module, file string) string {
	return path.Join(module, file)
}

// validMutationFiles accepts the host's selection only as clean, module-relative
// paths of non-test Go files, so an argument cannot point outside the module.
func validMutationFiles(files []string) error {
	if len(files) == 0 {
		return fmt.Errorf("go-mutation needs at least one file; the CLI skips the check when nothing changed")
	}
	for _, file := range files {
		if !filepath.IsLocal(file) || path.Clean(file) != file || strings.Contains(file, "\\") || !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
			return fmt.Errorf("invalid go-mutation file %q", file)
		}
	}
	if !slices.IsSorted(files) || len(slices.Compact(slices.Clone(files))) != len(files) {
		return fmt.Errorf("go-mutation files must be sorted and unique")
	}
	return nil
}

// GoMutation mutates the given files of a module with pinned gremlins and
// fails when a test covers a mutant but does not catch it. The CLI chooses the
// files, because the source arrives here without .git.
func (m *Levenshtein) GoMutation(ctx context.Context,
	// +optional
	// +defaultPath="/"
	// +ignore=["**/.env", "**/.env.*", "!**/.env.example", "**/.git"]
	source *dagger.Directory,
	// +default="."
	module string,
	files []string,
	// Changed-line ranges per file as JSON; empty counts every line.
	// +optional
	lines string,
	// +default=".levenshtein/mutation-accepted.json"
	accepted string,
	// +optional
	tags string,
	// +optional
	nonce string,
) (string, error) {
	if !filepath.IsLocal(module) || path.Clean(module) != module || strings.Contains(module, "\\") {
		return "", fmt.Errorf("invalid module path %q", module)
	}
	if !filepath.IsLocal(accepted) || path.Clean(accepted) != accepted {
		return "", fmt.Errorf("invalid accepted survivors path %q", accepted)
	}
	if err := validMutationFiles(files); err != nil {
		return "", err
	}
	changed, err := parseLines(lines, files)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(tags, " \t\n\x00") {
		return "", fmt.Errorf("invalid go-mutation tags %q", tags)
	}

	var tools toolchain
	if err := json.Unmarshal(toolchainJSON, &tools); err != nil {
		return "", err
	}
	verdict, err := mutate(ctx, source, module, tools, files, changed, accepted, tags, nonce)
	if err != nil {
		return "", err
	}
	summary, err := json.Marshal(verdict.Summary)
	if err != nil {
		return "", err
	}
	if len(verdict.Findings) == 0 {
		return string(summary), nil
	}
	return "", &gqlerror.Error{Message: "mutation testing failed", Extensions: map[string]any{
		"levenshteinFindings":   verdict.Findings,
		"levenshteinIncomplete": verdict.Incomplete,
		"levenshteinSummary":    string(summary),
	}}
}

// mutate runs gremlins over the selected files and decides the verdict. The
// self-test calls it directly on its fixtures.
func mutate(ctx context.Context, source *dagger.Directory, module string, tools toolchain, files []string, lines map[string][]lineRange, accepted, tags, nonce string) (mutationVerdict, error) {
	in, all, err := mutationSources(ctx, source, module, files, accepted)
	if err != nil {
		return mutationVerdict{}, err
	}
	in.Lines = lines
	patterns := exclusions(all, files)

	// A fast coverage run makes limits too short to trust, and only a run
	// that timed out a mutant under one needs to be repeated.
	coefficient := gremlinsTimeoutCoef
	for attempt := 1; ; attempt++ {
		run, err := runGremlins(ctx, source, module, tools, patterns, tags, nonce, coefficient)
		if err != nil {
			return mutationVerdict{}, err
		}
		longer, retry := longerCoefficient(run)
		if !retry || attempt == gremlinsAttempts {
			return decideMutation(run, in)
		}
		coefficient = longer
	}
}

// mutationSources reads what the verdict compares against: each selected
// file's lines, the accepted survivors, and every non-test Go file in the
// module so the rest can be excluded.
func mutationSources(ctx context.Context, source *dagger.Directory, module string, files []string, accepted string) (mutationInput, []string, error) {
	moduleDir := source.Directory(module)
	sources := map[string][]string{}
	for _, file := range files {
		contents, err := moduleDir.File(file).Contents(ctx)
		if err != nil {
			return mutationInput{}, nil, fmt.Errorf("selected file %q is not in the module: %w", file, err)
		}
		sources[file] = strings.Split(contents, "\n")
	}

	present, err := source.Exists(ctx, accepted, dagger.DirectoryExistsOpts{ExpectedType: dagger.ExistsTypeRegularType})
	if err != nil {
		return mutationInput{}, nil, err
	}
	text := ""
	if present {
		if text, err = source.File(accepted).Contents(ctx); err != nil {
			return mutationInput{}, nil, err
		}
	}
	entries, err := parseAccepted(text, present)
	if err != nil {
		return mutationInput{}, nil, err
	}

	listed, err := moduleDir.Glob(ctx, "**/*.go")
	if err != nil {
		return mutationInput{}, nil, err
	}
	var all []string
	for _, file := range listed {
		if !strings.HasSuffix(file, "_test.go") {
			all = append(all, file)
		}
	}
	return mutationInput{Module: module, Files: files, Sources: sources, Accepted: entries, AcceptedPath: accepted, AcceptedText: text}, all, nil
}

// runGremlins builds the pinned gremlins and runs it once from the module root.
// One invocation means one coverage pass; gremlins then tests each mutant
// against its own package only.
func runGremlins(ctx context.Context, source *dagger.Directory, module string, tools toolchain, patterns []string, tags, nonce string, coefficient int) (mutationRun, error) {
	ctr := goContainer(tools).
		WithDirectory("/tools", dag.CurrentModule().Source().Directory("tools")).
		WithWorkdir("/tools").
		WithExec([]string{"go", "build", "-trimpath", "-o", "/usr/local/bin/gremlins", "github.com/go-gremlins/gremlins/cmd/gremlins"}).
		WithDirectory("/src", source).
		WithDirectory(gremlinsReportDir, dag.Directory()).
		WithWorkdir(path.Join("/src", module)).
		WithEnvVariable("NO_COLOR", "1")
	// Gremlins passes its environment to every go command it runs.
	ctr = ctr.WithEnvVariable("GOFLAGS", gremlinsGoFlags)
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}

	checked := ctr.WithExec(gremlinsCommand(coefficient, tags, patterns), dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
	exitCode, err := checked.ExitCode(ctx)
	if err != nil {
		return mutationRun{}, err
	}
	stdout, err := checked.Stdout(ctx)
	if err != nil {
		return mutationRun{}, err
	}
	stderr, err := checked.Stderr(ctx)
	if err != nil {
		return mutationRun{}, err
	}
	entries, err := checked.Directory(gremlinsReportDir).Entries(ctx)
	if err != nil {
		return mutationRun{}, err
	}
	run := mutationRun{ExitCode: exitCode, Stdout: stdout, Stderr: stderr, Coefficient: coefficient}
	if !slices.Contains(entries, path.Base(gremlinsReportPath)) {
		return run, nil
	}
	report, err := checked.File(gremlinsReportPath).Contents(ctx)
	if err != nil {
		return mutationRun{}, err
	}
	return mutationRun{ExitCode: exitCode, Stdout: stdout, Stderr: stderr, Report: report, Reported: true, Coefficient: coefficient}, nil
}

// gremlinsCommand is one gremlins invocation over the module.
func gremlinsCommand(coefficient int, tags string, patterns []string) []string {
	command := []string{"gremlins", "unleash", ".", "--workers", gremlinsWorkers, "--timeout-coefficient", strconv.Itoa(coefficient), "--output", gremlinsReportPath}
	if tags != "" {
		command = append(command, "--tags", tags)
	}
	for _, pattern := range patterns {
		command = append(command, "-E", pattern)
	}
	return command
}
