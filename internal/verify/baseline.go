package verify

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// baselineStaleCode is the finding a baseline entry becomes when the findings
// it records no longer occur, so fixing a baselined finding also removes it
// from the file.
const baselineStaleCode = "baseline-stale"

// baselineKinds are the kinds whose findings each name one source location
// and a message that does not repeat it. go-vet, workflow-lint and the other
// tool kinds report one finding per module carrying the tool's whole output,
// line numbers included, which no baseline entry could match across unrelated
// edits; go-mutation has its own accepted-survivors file.
var baselineKinds = map[CheckKind]bool{
	CheckGoLint:    true,
	CheckGoHTTP:    true,
	CheckGoSQL:     true,
	CheckGoImports: true,
	CheckShellLint: true,
}

// Baseline is the checked-in record of findings a repository has accepted for
// now. Path is repository-relative; lines holds the line of each entry in the
// file it was read from, so a stale entry can be reported where it is.
type Baseline struct {
	Path    string
	Entries []BaselineEntry
	lines   []int
}

// BaselineEntry records how many findings with one kind, target directory,
// file, code and normalized message a repository accepts. It has no line
// number, so edits elsewhere in the file do not break it.
type BaselineEntry struct {
	Kind    CheckKind `json:"kind"`
	Dir     string    `json:"dir"`
	File    string    `json:"file"`
	Code    string    `json:"code"`
	Message string    `json:"message"`
	Count   int       `json:"count"`
}

// BaselineSummary is what a report says about the baseline it applied.
type BaselineSummary struct {
	File      string `json:"file"`
	Baselined int    `json:"baselined"`
	Stale     int    `json:"stale"`
}

// BaselineChange is what recording a run changed in the file: findings added
// and removed, counting each occurrence, and the checks whose findings a
// baseline cannot hold.
type BaselineChange struct {
	Added      int
	Removed    int
	Unrecorded []string
}

type baselineFile struct {
	Version  int             `json:"version"`
	Findings []BaselineEntry `json:"findings"`
}

// baselineKey is what a finding and an entry must share to match.
type baselineKey struct {
	Kind    CheckKind
	Dir     string
	File    string
	Code    string
	Message string
}

func (e BaselineEntry) key() baselineKey {
	return baselineKey{Kind: e.Kind, Dir: e.Dir, File: e.File, Code: e.Code, Message: normalizeMessage(e.Message)}
}

func findingKey(check PlannedCheck, f finding) baselineKey {
	return baselineKey{Kind: check.Check.Kind, Dir: check.Target.Dir, File: f.Location.File, Code: f.Code, Message: normalizeMessage(f.Message)}
}

// goPosition is a Go source position inside a message, such as "x.go:12:3".
var goPosition = regexp.MustCompile(`\.go:\d+(:\d+)?`)

// normalizeMessage drops Go source positions from a message and collapses
// whitespace, so a message that mentions another line still matches after
// that line moves.
func normalizeMessage(message string) string {
	return strings.Join(strings.Fields(goPosition.ReplaceAllString(message, ".go")), " ")
}

func validBaselinePath(path string) error {
	if !relative(path) || path == "." || privateSourcePath(path) {
		return fmt.Errorf("baseline file %q must be a clean repository-relative path", path)
	}
	return nil
}

// LoadBaseline reads the baseline file at a repository-relative path. A
// missing file records nothing, which is how a repository starts.
func LoadBaseline(source, path string) (Baseline, error) {
	if err := validBaselinePath(path); err != nil {
		return Baseline{}, err
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		return Baseline{}, err
	}
	defer func() { _ = root.Close() }() // Read-only directory handle.

	data, err := root.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Baseline{Path: path}, nil
	}
	if err != nil {
		return Baseline{}, err
	}
	entries, lines, err := parseBaseline(data)
	if err != nil {
		return Baseline{}, fmt.Errorf("baseline file %q: %w", path, err)
	}
	return Baseline{Path: path, Entries: entries, lines: lines}, nil
}

func parseBaseline(data []byte) ([]BaselineEntry, []int, error) {
	var file baselineFile
	if err := decode(data, &file); err != nil {
		return nil, nil, err
	}
	if file.Version != 1 {
		return nil, nil, fmt.Errorf(`needs "version": 1`)
	}

	seen := map[baselineKey]bool{}
	for i, entry := range file.Findings {
		if err := validateEntry(entry); err != nil {
			return nil, nil, fmt.Errorf("entry %d: %w", i+1, err)
		}
		if seen[entry.key()] {
			return nil, nil, fmt.Errorf("entry %d repeats an earlier entry; merge them into one with a count", i+1)
		}
		seen[entry.key()] = true
	}
	return file.Findings, entryLines(data, len(file.Findings)), nil
}

func validateEntry(entry BaselineEntry) error {
	if !baselineKinds[entry.Kind] {
		return fmt.Errorf("kind %q cannot be baselined; only %s findings can", entry.Kind, baselineKindNames())
	}
	if !relative(entry.Dir) {
		return fmt.Errorf("dir %q must be a clean repository-relative path", entry.Dir)
	}
	if !relative(entry.File) || entry.File == "." {
		return fmt.Errorf("file %q must be a clean repository-relative path", entry.File)
	}
	if entry.Code == "" || normalizeMessage(entry.Message) == "" {
		return fmt.Errorf("needs a code and a message")
	}
	if entry.Count < 1 {
		return fmt.Errorf("count must be at least 1")
	}
	return nil
}

func baselineKindNames() string {
	names := make([]string, 0, len(baselineKinds))
	for kind := range baselineKinds {
		names = append(names, string(kind))
	}
	slices.Sort(names)
	return strings.Join(names, ", ")
}

// entryLines finds the line each entry of the findings array starts on. The
// file already decoded strictly, so a surprise here only costs the lines,
// which then fall back to 1.
func entryLines(data []byte, count int) []int {
	lines := make([]int, count)
	for i := range lines {
		lines[i] = 1
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	if _, err := decoder.Token(); err != nil {
		return lines
	}
	for decoder.More() {
		key, err := decoder.Token()
		if err != nil {
			return lines
		}
		if key != "findings" {
			var skip json.RawMessage
			if decoder.Decode(&skip) != nil {
				return lines
			}
			continue
		}
		if _, err := decoder.Token(); err != nil {
			return lines
		}
		for i := 0; decoder.More() && i < count; i++ {
			// The decoder's offset is just past the previous entry, before the
			// comma and whitespace that lead to this one.
			offset := int(decoder.InputOffset())
			start := len(data) - len(bytes.TrimLeft(data[offset:], " \t\r\n,"))
			lines[i] = 1 + bytes.Count(data[:start], []byte("\n"))
			var skip json.RawMessage
			if decoder.Decode(&skip) != nil {
				return lines
			}
		}
		return lines
	}
	return lines
}

// completed reports whether a result is a verdict over everything its check
// looked at, which is what judging a baseline against it needs.
func completed(result Result) bool {
	return result.Status == StatusPassed || result.Status == StatusFailed
}

// covers reports whether an entry belongs to a planned check: same kind and
// same target directory.
func covers(check PlannedCheck, entry BaselineEntry) bool {
	return check.Check.Kind == entry.Kind && check.Target.Dir == entry.Dir
}

// Apply marks the findings the baseline records as baselined, adds a stale
// finding for each entry that records more findings than a check that covers
// it found, and recomputes every affected status. Only checks of a baseline
// kind that completed are touched: an entry is judged only when a check of its
// kind ran over its target directory and reached a verdict. When several such
// checks ran, each matches the entry on its own, and it is stale only when
// every one of them left part of it unused.
func (b Baseline) Apply(report Report) Report {
	checks := report.planned()
	results := slices.Clone(report.Results)
	unused := map[int][]int{}
	firstCover := map[int]int{}
	baselined := 0

	for i, result := range results {
		check, ok := checks[result.ID]
		if !ok || !baselineKinds[check.Check.Kind] || !completed(result) {
			continue
		}

		left := map[baselineKey]int{}
		var covering []int
		for index, entry := range b.Entries {
			if covers(check, entry) {
				left[entry.key()] = entry.Count
				covering = append(covering, index)
			}
		}

		findings := detailFindings(result.Details)
		for j, f := range findings {
			key := findingKey(check, f)
			if f.Code != baselineStaleCode && !f.Advisory && left[key] > 0 {
				left[key]--
				findings[j].Baselined = true
				baselined++
			}
		}
		if len(findings) != 0 {
			results[i] = result.withDetails(replaceFindings(result.Details, findings))
		}

		for _, index := range covering {
			unused[index] = append(unused[index], left[b.Entries[index].key()])
			if _, seen := firstCover[index]; !seen {
				firstCover[index] = i
			}
		}
	}

	stale := map[int][]finding{}
	staleCount := 0
	for index, counts := range unused {
		if missing := slices.Min(counts); missing > 0 {
			stale[firstCover[index]] = append(stale[firstCover[index]], b.staleFinding(index, missing))
			staleCount++
		}
	}

	for i, result := range results {
		check, ok := checks[result.ID]
		if !ok || !baselineKinds[check.Check.Kind] || !completed(result) {
			continue
		}
		results[i] = judged(result, stale[i])
	}

	report.Results = results
	report.Status = reportStatus(results)
	report.Baseline = &BaselineSummary{File: b.Path, Baselined: baselined, Stale: staleCount}
	return report
}

// judged settles one completed result once its findings are marked: it fails
// while any finding is neither baselined, advisory, nor absent, and passes
// otherwise.
func judged(result Result, stale []finding) Result {
	slices.SortFunc(stale, func(a, b finding) int {
		return cmp.Or(cmp.Compare(a.Location.Line, b.Location.Line), cmp.Compare(a.Message, b.Message))
	})
	findings := append(detailFindings(result.Details), stale...)
	if len(stale) != 0 {
		result = result.withDetails(replaceFindings(result.Details, findings))
	}

	failing := slices.ContainsFunc(findings, func(f finding) bool { return fails(f) && f.Code != baselineStaleCode })
	switch {
	case failing:
		return result.withOutcome(StatusFailed, result.Error)
	case len(stale) != 0:
		return result.withOutcome(StatusFailed, "baseline entries no longer match; remove them")
	default:
		return result.withOutcome(StatusPassed, "")
	}
}

func (b Baseline) staleFinding(index, missing int) finding {
	entry := b.Entries[index]
	line := 1
	if index < len(b.lines) {
		line = b.lines[index]
	}
	return finding{
		Code:     baselineStaleCode,
		Message:  fmt.Sprintf("%d of %d baselined %s findings in %s no longer occur: %s", missing, entry.Count, entry.Code, entry.File, entry.Message),
		Location: location{File: b.Path, Line: line},
	}
}

// Record returns the baseline that holds exactly the failing findings a run
// reported for every baseline-kind check it ran, and keeps the entries of kinds and
// directories the run did not cover. It refuses a run in which any check did
// not reach a verdict, because its findings are unknown. When several checks
// cover the same entry, it records the most findings any one of them reported.
func (b Baseline) Record(report Report) (Baseline, BaselineChange, error) {
	var unfinished []string
	for _, result := range report.Results {
		if !completed(result) {
			unfinished = append(unfinished, fmt.Sprintf("%s (%s)", result.ID, result.Status))
		}
	}
	if len(unfinished) != 0 || len(report.Results) == 0 {
		return Baseline{}, BaselineChange{}, fmt.Errorf("not every check reached a verdict, so the baseline was not written: %s", strings.Join(unfinished, ", "))
	}

	type scope struct {
		Kind CheckKind
		Dir  string
	}
	covered := map[scope]bool{}
	counts := map[baselineKey]int{}
	var change BaselineChange
	checks := report.planned()
	for _, result := range report.Results {
		check := checks[result.ID]
		findings := detailFindings(result.Details)
		if !baselineKinds[check.Check.Kind] {
			if result.Status == StatusFailed {
				change.Unrecorded = append(change.Unrecorded, result.ID)
			}
			continue
		}

		covered[scope{Kind: check.Check.Kind, Dir: check.Target.Dir}] = true
		local := map[baselineKey]int{}
		for _, f := range findings {
			if f.Code != baselineStaleCode && !f.Advisory {
				local[findingKey(check, f)]++
			}
		}
		for key, count := range local {
			counts[key] = max(counts[key], count)
		}
	}

	var entries []BaselineEntry
	before := map[baselineKey]int{}
	for _, entry := range b.Entries {
		before[entry.key()] = entry.Count
		if !covered[scope{Kind: entry.Kind, Dir: entry.Dir}] {
			counts[entry.key()] = entry.Count
		}
	}
	for key, count := range counts {
		entries = append(entries, BaselineEntry{Kind: key.Kind, Dir: key.Dir, File: key.File, Code: key.Code, Message: key.Message, Count: count})
		change.Added += max(0, count-before[key])
	}
	for key, count := range before {
		change.Removed += max(0, count-counts[key])
	}
	slices.SortFunc(entries, compareEntries)
	return Baseline{Path: b.Path, Entries: entries}, change, nil
}

func compareEntries(a, b BaselineEntry) int {
	return cmp.Or(
		cmp.Compare(a.Kind, b.Kind),
		cmp.Compare(a.Dir, b.Dir),
		cmp.Compare(a.File, b.File),
		cmp.Compare(a.Code, b.Code),
		cmp.Compare(a.Message, b.Message),
	)
}

// Encode writes the baseline one entry per line, sorted, so a change to it
// reads as added and removed lines.
func (b Baseline) Encode() ([]byte, error) {
	var out bytes.Buffer
	out.WriteString("{\n  \"version\": 1,\n  \"findings\": [")
	for i, entry := range b.Entries {
		var line bytes.Buffer
		encoder := json.NewEncoder(&line)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(entry); err != nil {
			return nil, err
		}
		if i > 0 {
			out.WriteString(",")
		}
		out.WriteString("\n    ")
		out.Write(bytes.TrimSuffix(line.Bytes(), []byte("\n")))
	}
	if len(b.Entries) != 0 {
		out.WriteString("\n  ")
	}
	out.WriteString("]\n}\n")
	return out.Bytes(), nil
}

// Write replaces the baseline file in the source repository atomically,
// refusing a path that runs through a symlink.
func (b Baseline) Write(source string) error {
	if err := validBaselinePath(b.Path); err != nil {
		return err
	}
	if err := checkOutputPath(source, b.Path); err != nil {
		return err
	}
	data, err := b.Encode()
	if err != nil {
		return err
	}

	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }() // Directory handle cleanup; the write is closed separately.
	if err := root.MkdirAll(filepath.Dir(b.Path), 0755); err != nil {
		return err
	}
	return atomicWriteRoot(root, b.Path, data, 0644)
}
