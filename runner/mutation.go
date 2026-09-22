package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"dagger/levenshtein/internal/dagger"

	"github.com/vektah/gqlparser/v2/gqlerror"
)

// Gremlins settings that decide results. A fixed worker count keeps load, and
// so the per-mutant time limits, the same from one machine to the next; the
// coefficient scales each limit from the coverage run's duration, which a warm
// build cache makes short enough to time out every mutant at the default of 3.
const (
	gremlinsWorkers     = "2"
	gremlinsTimeoutCoef = "10"
	gremlinsReportDir   = "/report"
	gremlinsReportPath  = gremlinsReportDir + "/out.json"
	gremlinsNoResults   = "No results to report."

	// minTimeoutsForIncomplete is how many covered mutants have to time out,
	// with none killed or surviving, before the run is blamed on the machine.
	minTimeoutsForIncomplete = 3

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
type acceptedEntry struct {
	File    string `json:"file"`
	Mutator string `json:"mutator"`
	Line    string `json:"line"`
	Reason  string `json:"reason"`
}

// mutationRun is what one gremlins invocation produced.
type mutationRun struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Report   string
	Reported bool
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
// it: a mutation that makes the code hang is one the tests noticed. A run is
// incomplete only when every covered mutant timed out, and there were enough of
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
			case mutationLived:
				lived = append(lived, mutant)
			case mutationNotCovered:
				summary.NotCovered++
				summary.Uncovered = append(summary.Uncovered, mutant)
			case mutationTimedOut:
				summary.TimedOut++
				timedOut = append(timedOut, mutant)
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

	// An entry is used when it matches a survivor. An entry for a file this
	// run mutated that matches no mutant at all is stale.
	used := make([]bool, len(in.Accepted))
	var findings []diagnostic
	for _, mutant := range sortedMutants(lived) {
		text := sourceLine(in.Sources, mutant)
		if i := acceptedIndex(in.Accepted, mutant, text); i >= 0 {
			used[i] = true
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
		if used[i] || !mutated[entry.File] || matchesAnyMutant(report, in.Sources, entry) {
			continue
		}
		findings = append(findings, diagnostic{
			Code:     "go-mutation-stale",
			Message:  fmt.Sprintf("accepted survivor for %s (%s, %q) no longer matches a mutant; remove it", entry.File, entry.Mutator, entry.Line),
			Location: location{File: in.AcceptedPath, Line: entryLine(in.AcceptedText, entry.Line)},
		})
	}
	// Hangs are deterministic, so a few mutants that all hang is a result, not
	// an environment failure; incomplete would fail that change on every run,
	// with nothing the accepted file could clear.
	survivors := summary.Lived + summary.Unchanged + summary.Accepted
	incomplete := summary.TimedOut >= minTimeoutsForIncomplete && summary.Killed == 0 && survivors == 0
	if incomplete {
		for _, mutant := range sortedMutants(timedOut) {
			findings = append(findings, diagnostic{
				Code:     "go-mutation-timeout",
				Message:  fmt.Sprintf("%s mutant timed out, like every other covered mutant, so the run gave no verdict: %s", mutant.Mutator, sourceLine(in.Sources, mutant)),
				Location: location{File: moduleFile(in.Module, mutant.File), Line: mutant.Line, Column: mutant.Column},
			})
		}
	}
	summary.Uncovered = sortedMutants(summary.Uncovered)
	summary.UnchangedList = sortedMutants(summary.UnchangedList)
	summary.TimedOutMutants = sortedMutants(summary.TimedOutMutants)
	return mutationVerdict{Findings: findings, Incomplete: incomplete, Summary: summary}, nil
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

func acceptedIndex(accepted []acceptedEntry, mutant mutationMutant, text string) int {
	return slices.IndexFunc(accepted, func(entry acceptedEntry) bool {
		return entry.File == mutant.File && entry.Mutator == mutant.Mutator && strings.TrimSpace(entry.Line) == text
	})
}

// matchesAnyMutant keeps an entry that names a mutant this run killed or could
// not cover from being called stale: the survivor it describes may come back.
func matchesAnyMutant(report gremlinsReport, sources map[string][]string, entry acceptedEntry) bool {
	for _, file := range report.Files {
		if file.FileName != entry.File {
			continue
		}
		for _, m := range file.Mutations {
			mutant := mutationMutant{File: file.FileName, Line: m.Line, Column: m.Column, Mutator: m.Type}
			if m.Type == entry.Mutator && sourceLine(sources, mutant) == strings.TrimSpace(entry.Line) {
				return true
			}
		}
	}
	return false
}

// entryLine finds the line of the accepted file that holds an entry's source
// text, so a stale finding points at the entry to delete.
func entryLine(text, line string) int {
	// Encode the way a person writes the file: json.Marshal would escape the
	// < and > that comparisons are full of.
	var encoded strings.Builder
	encoder := json.NewEncoder(&encoded)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(line) // A string always encodes.
	quoted := strings.TrimSpace(encoded.String())
	for i, candidate := range strings.Split(text, "\n") {
		if strings.Contains(candidate, quoted) {
			return i + 1
		}
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
	run, err := runGremlins(ctx, source, module, tools, exclusions(all, files), tags, nonce)
	if err != nil {
		return mutationVerdict{}, err
	}
	return decideMutation(run, in)
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
func runGremlins(ctx context.Context, source *dagger.Directory, module string, tools toolchain, patterns []string, tags, nonce string) (mutationRun, error) {
	ctr := goContainer(tools).
		WithDirectory("/tools", dag.CurrentModule().Source().Directory("tools")).
		WithWorkdir("/tools").
		WithExec([]string{"go", "build", "-trimpath", "-o", "/usr/local/bin/gremlins", "github.com/go-gremlins/gremlins/cmd/gremlins"}).
		WithDirectory("/src", source).
		WithDirectory(gremlinsReportDir, dag.Directory()).
		WithWorkdir(path.Join("/src", module)).
		WithEnvVariable("NO_COLOR", "1")
	if nonce != "" {
		ctr = ctr.WithEnvVariable("LEVENSHTEIN_RUN_NONCE", nonce)
	}

	command := []string{"gremlins", "unleash", ".", "--workers", gremlinsWorkers, "--timeout-coefficient", gremlinsTimeoutCoef, "--output", gremlinsReportPath}
	if tags != "" {
		command = append(command, "--tags", tags)
	}
	for _, pattern := range patterns {
		command = append(command, "-E", pattern)
	}

	checked := ctr.WithExec(command, dagger.ContainerWithExecOpts{Expect: dagger.ReturnTypeAny})
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
	run := mutationRun{ExitCode: exitCode, Stdout: stdout, Stderr: stderr}
	if !slices.Contains(entries, path.Base(gremlinsReportPath)) {
		return run, nil
	}
	report, err := checked.File(gremlinsReportPath).Contents(ctx)
	if err != nil {
		return mutationRun{}, err
	}
	return mutationRun{ExitCode: exitCode, Stdout: stdout, Stderr: stderr, Report: report, Reported: true}, nil
}
