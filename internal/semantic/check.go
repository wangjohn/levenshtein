package semantic

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/wangjohn/levenshtein/internal/gitchange"
	"sync"
)

// Mode records how findings affect the check outcome. Advisory findings never do.
type Mode string

const ModeAdvisory Mode = "advisory"

// Options describe one semantic-lint execution.
type Options struct {
	Source      string
	Include     func(path string) bool
	Git         string
	Env         []string
	Base        string
	Client      Client
	Concurrency int
	// MaxRequests and MaxInputChars bound what one run sends; zero selects
	// DefaultMaxRequests and DefaultMaxInputChars.
	MaxRequests   int
	MaxInputChars int
}

// The default budgets keep a large refactor to about as many requests as the
// default five-minute timeout can answer, and to a bounded bill.
const (
	DefaultMaxRequests   = 60
	DefaultMaxInputChars = 1_500_000
)

// Finding is a judgment that crossed its threshold.
type Finding struct {
	Question   string   `json:"question"`
	Severity   Severity `json:"severity"`
	Path       string   `json:"path,omitempty"`
	Line       int      `json:"line,omitempty"`
	Symbol     string   `json:"symbol,omitempty"`
	Value      float64  `json:"value"`
	Confidence *float64 `json:"confidence,omitempty"`
	Threshold  float64  `json:"threshold"`
	Message    string   `json:"message"`
}

// Judgment is every raw answer, kept for threshold calibration.
type Judgment struct {
	Question   string   `json:"question"`
	Path       string   `json:"path,omitempty"`
	Line       int      `json:"line,omitempty"`
	Symbol     string   `json:"symbol,omitempty"`
	Value      float64  `json:"value"`
	Confidence *float64 `json:"confidence,omitempty"`
	Fired      bool     `json:"fired"`
	Uncertain  bool     `json:"uncertain,omitempty"`
}

// Report is the check's structured detail record.
type Report struct {
	Version     int        `json:"version"`
	Mode        Mode       `json:"mode"`
	Catalog     string     `json:"catalog"`
	Model       string     `json:"model"`
	ServedModel string     `json:"served_model,omitempty"`
	BaseRef     string     `json:"base_ref,omitempty"`
	Base        string     `json:"base,omitempty"`
	Requests    int        `json:"requests"`
	InputTokens int        `json:"input_tokens"`
	Findings    []Finding  `json:"findings"`
	Judgments   []Judgment `json:"judgments"`
	// Missing names each question that was asked and not answered, as
	// "path:line symbol question".
	Missing []string `json:"missing,omitempty"`
	// Skipped names each state the budgets left unsent.
	Skipped []string `json:"skipped,omitempty"`
	// Errors are the distinct reasons requests failed.
	Errors []string `json:"errors,omitempty"`
	Notes  []string `json:"notes,omitempty"`
}

// pending maps a wire question back to the catalog entry and location it judges.
type pending struct {
	question Question
	path     string
	line     int
	symbol   string
}

type request struct {
	state     map[string]any
	questions map[string]wireQuestion
	pending   map[string]pending
	// oversize explains why the state could not be shrunk under the limit.
	// Such a request is reported as a note instead of being sent.
	oversize string
}

const (
	maxStateChars      = 110000
	maxDocsDiffChars   = 30000
	minHelperLines     = 3
	defaultConcurrency = 4
)

// Run diffs the source against its base, builds one state per touched unit,
// asks the catalog's questions, and composes advisory findings.
func Run(ctx context.Context, opts Options) (Report, error) {
	report := Report{Version: 1, Mode: ModeAdvisory, Catalog: CatalogVersion, Model: opts.Client.Model, Findings: []Finding{}, Judgments: []Judgment{}}
	git := gitRunner{Git: opts.Git, Dir: opts.Source, Env: opts.Env}

	top, err := git.Run(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return report, err
	}
	prefix, err := gitchange.SourcePrefix(strings.TrimSpace(top), opts.Source)
	if err != nil {
		return report, err
	}
	include := func(path string) bool {
		rel, ok := strings.CutPrefix(filepath.ToSlash(path), prefix)
		return ok && opts.Include(filepath.FromSlash(rel))
	}

	change, err := loadChange(ctx, git, opts.Base, include)
	if err != nil {
		return report, err
	}
	report.BaseRef, report.Base = change.BaseRef, change.Base

	requests, notes := buildRequests(opts, change, prefix)
	report.Notes = notes
	if len(requests) == 0 && len(notes) == 0 {
		report.Notes = append(report.Notes, "no reviewable Go or Markdown changes against "+change.BaseRef)
		return report, nil
	}
	requests, skipped, note := withinBudget(requests, opts)
	if note != "" {
		report.Notes = append(report.Notes, note)
		report.Skipped = skipped
	}

	// A failed request costs only its own questions: the rest are composed,
	// and its questions are reported as unanswered with the reason.
	responses, errs := askAll(ctx, opts, requests)
	for i, r := range requests {
		if errs[i] != nil {
			report.Errors = appendUnique(report.Errors, failure(errs[i]))
			compose(&report, r, nil)
			continue
		}
		response := responses[i]
		report.Requests++
		report.InputTokens += response.Usage.InputTokens
		if response.Model != "" {
			if report.ServedModel != "" && report.ServedModel != response.Model {
				report.Notes = append(report.Notes, fmt.Sprintf("served model changed from %s to %s during the run", report.ServedModel, response.Model))
			}
			report.ServedModel = response.Model
		}
		compose(&report, r, response.Answers)
	}

	sort.SliceStable(report.Findings, func(i, j int) bool {
		return severityRank(report.Findings[i].Severity) < severityRank(report.Findings[j].Severity)
	})
	sort.Strings(report.Missing)
	report.Missing = slices.Compact(report.Missing)
	return report, nil
}

// withinBudget keeps requests, in order, while both budgets allow, and names
// the rest in one note rather than sending them.
func withinBudget(requests []request, opts Options) ([]request, []string, string) {
	maxRequests := cmp.Or(opts.MaxRequests, DefaultMaxRequests)
	maxChars := cmp.Or(opts.MaxInputChars, DefaultMaxInputChars)

	var kept []request
	var skipped []string
	chars := 0
	limit := ""
	for _, r := range requests {
		size := r.chars()
		switch {
		case len(kept) >= maxRequests:
			limit = cmp.Or(limit, fmt.Sprintf("max_requests (%d)", maxRequests))
		case chars+size > maxChars:
			limit = cmp.Or(limit, fmt.Sprintf("max_input_chars (%d)", maxChars))
		default:
			kept = append(kept, r)
			chars += size
			continue
		}
		skipped = appendUnique(skipped, r.label())
	}
	if len(skipped) == 0 {
		return kept, nil, ""
	}
	return kept, skipped, fmt.Sprintf("%d of %d requests were not sent because they would exceed %s; raise the budget in the check's semantic options to judge %s", len(requests)-len(kept), len(requests), limit, strings.Join(firstN(skipped, 5), ", "))
}

func firstN(values []string, n int) []string {
	if len(values) <= n {
		return values
	}
	return append(slices.Clone(values[:n]), fmt.Sprintf("%d more", len(values)-n))
}

func appendUnique(values []string, value string) []string {
	if slices.Contains(values, value) {
		return values
	}
	return append(values, value)
}

// failure words a request error for the report; the check's own deadline is
// the common case and deserves a plain name.
func failure(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out before every request was answered"
	case errors.Is(err, context.Canceled):
		return "cancelled before every request was answered"
	}
	return err.Error()
}

// askAll sends requests with bounded concurrency. Each request succeeds or
// fails on its own; once ctx ends, the requests not yet sent fail with its
// error. Responses and errors keep request order so composition stays
// deterministic.
func askAll(ctx context.Context, opts Options, requests []request) ([]wireResponse, []error) {
	workers := opts.Concurrency
	if workers <= 0 {
		workers = defaultConcurrency
	}

	responses := make([]wireResponse, len(requests))
	errs := make([]error, len(requests))
	slots := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i, r := range requests {
		if err := ctx.Err(); err != nil {
			errs[i] = err
			continue
		}
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
			errs[i] = ctx.Err()
			continue
		}
		wg.Add(1)
		go func(i int, r request) {
			defer wg.Done()
			defer func() { <-slots }()
			responses[i], errs[i] = opts.Client.Ask(ctx, r.state, r.questions)
		}(i, r)
	}
	wg.Wait()
	return responses, errs
}

func buildRequests(opts Options, change Change, prefix string) ([]request, []string) {
	var requests []request
	var notes []string
	var docsDiff strings.Builder
	var addedSymbols, addedKeys []string

	for _, file := range change.Files {
		rel := strings.TrimPrefix(filepath.ToSlash(file.Path), prefix)
		full := filepath.Join(opts.Source, filepath.FromSlash(rel))
		addedKeys = append(addedKeys, configKeys(file)...)
		if file.Status == FileDeleted {
			continue
		}

		switch file.Kind {
		case FileSource, FileTest:
			src, err := os.ReadFile(full)
			if err != nil {
				notes = append(notes, fmt.Sprintf("%s: %v", rel, err))
				continue
			}
			units := goUnits(rel, src, file.Hunks)
			exclude := map[string]bool{}
			for _, unit := range units {
				exclude[unit.Symbol] = true
				if unit.New {
					addedSymbols = append(addedSymbols, rel+":"+unit.Symbol)
				}
			}
			var neighbours []string
			for _, unit := range units {
				if unit.IsFunc && unit.AddedLines >= minHelperLines && neighbours == nil {
					neighbours = packageFunctions(filepath.Dir(full), full, file.Kind == FileTest, exclude)
				}
				if r, ok := goRequest(unit, file.Kind == FileTest, neighbours); ok {
					requests, notes = keep(requests, notes, r)
				}
			}
		case FileDocs:
			src, err := os.ReadFile(full)
			if err != nil {
				notes = append(notes, fmt.Sprintf("%s: %v", rel, err))
				continue
			}
			for _, hunk := range file.Hunks {
				docsDiff.WriteString(hunk.Diff)
			}
			for _, unit := range docUnits(rel, src, file.Hunks) {
				if r, ok := docRequest(unit); ok {
					requests, notes = keep(requests, notes, r)
				}
			}
		case FileConfig, FileWorkflow, FileOther:
			// Only Go and Markdown carry judged units; other kinds still shape the change summary.
		}
	}

	// The change-level request goes first, so a tight budget still judges the
	// change as a whole before any single hunk.
	if r, ok := changeRequest(change, prefix, truncate(docsDiff.String(), maxDocsDiffChars), addedSymbols, addedKeys); ok {
		var first []request
		first, notes = keep(nil, notes, r)
		requests = append(first, requests...)
	}
	return requests, notes
}

var (
	structTagKey  = regexp.MustCompile(`json:"([A-Za-z0-9_]+)`)
	jsonObjectKey = regexp.MustCompile(`^\s*"([A-Za-z0-9_.-]+)"\s*:`)
)

// configKeys lists identifiers a user might need documented: new struct tags
// and new keys in JSON configuration files.
func configKeys(file FileChange) []string {
	var keys []string
	for _, hunk := range file.Hunks {
		for _, line := range hunk.Added {
			pattern := structTagKey
			if file.Kind == FileConfig {
				pattern = jsonObjectKey
			}
			if match := pattern.FindStringSubmatch(line); match != nil {
				keys = append(keys, match[1])
			}
		}
	}
	return keys
}

func goRequest(unit goUnit, isTest bool, neighbours []string) (request, bool) {
	items := map[string]any{
		string(ItemsComments): unit.Comments,
		string(ItemsErrors):   unit.Errors,
	}
	counts := map[Items]int{
		ItemsComments: len(unit.Comments),
		ItemsErrors:   len(unit.Errors),
	}
	lines := map[Items][]int{
		ItemsComments: lineNumbers(len(unit.Comments), func(i int) int { return unit.Comments[i].Line }),
		ItemsErrors:   lineNumbers(len(unit.Errors), func(i int) int { return unit.Errors[i].Line }),
	}

	compareHelpers := unit.IsFunc && unit.AddedLines >= minHelperLines && len(neighbours) > 0
	state := map[string]any{
		"file":  map[string]any{"path": unit.Path, "is_test": isTest},
		"hunk":  map[string]any{"diff": unit.Diff, "after": unit.After, "symbol": unit.Symbol, "line": unit.Line},
		"items": items,
	}
	if compareHelpers {
		state["neighbours"] = map[string]any{"package_functions": neighbours}
	}

	r := request{state: state, questions: map[string]wireQuestion{}, pending: map[string]pending{}}
	for _, q := range questionsFor(ScopeGoHunk) {
		if q.Items == ItemsNone {
			if compareHelpers {
				r.add(q, -1, pending{question: q, path: unit.Path, line: unit.Line, symbol: unit.Symbol})
			}
			continue
		}
		for i := range counts[q.Items] {
			r.add(q, i, pending{question: q, path: unit.Path, line: lines[q.Items][i], symbol: unit.Symbol})
		}
	}
	if err := r.fit(); err != nil {
		r.oversize = err.Error()
	}
	return r, len(r.questions) > 0
}

func lineNumbers(count int, at func(int) int) []int {
	numbers := make([]int, count)
	for i := range numbers {
		numbers[i] = at(i)
	}
	return numbers
}

func docRequest(unit docUnit) (request, bool) {
	r := request{
		state: map[string]any{
			"file":  map[string]any{"path": unit.Path},
			"hunk":  map[string]any{"diff": unit.Diff, "after": unit.After, "line": unit.Line},
			"items": map[string]any{string(ItemsSentences): unit.Sentences},
		},
		questions: map[string]wireQuestion{},
		pending:   map[string]pending{},
	}
	for _, q := range questionsFor(ScopeDocsHunk) {
		if q.Items == ItemsNone {
			if unit.Prose {
				r.add(q, -1, pending{question: q, path: unit.Path, line: unit.Line})
			}
			continue
		}
		for i, sentence := range unit.Sentences {
			r.add(q, i, pending{question: q, path: unit.Path, line: sentence.Line})
		}
	}
	if err := r.fit(); err != nil {
		r.oversize = err.Error()
	}
	return r, len(r.questions) > 0
}

type fileSummary struct {
	Path        string     `json:"path"`
	Kind        FileKind   `json:"kind"`
	Status      FileStatus `json:"status"`
	Added       int        `json:"added"`
	Removed     int        `json:"removed"`
	HunkHeaders []string   `json:"hunk_headers"`
}

func changeRequest(change Change, prefix, docsDiff string, addedSymbols, addedKeys []string) (request, bool) {
	var files []fileSummary
	counts := map[FileKind]int{}
	var subjects []string
	for _, file := range change.Files {
		var headers []string
		for _, hunk := range file.Hunks {
			headers = append(headers, fmt.Sprintf("@@ -%d,%d +%d,%d @@ %s", hunk.OldStart, hunk.OldLines, hunk.NewStart, hunk.NewLines, hunk.Header))
		}
		files = append(files, fileSummary{Path: strings.TrimPrefix(filepath.ToSlash(file.Path), prefix), Kind: file.Kind, Status: file.Status, Added: file.Added, Removed: file.Removed, HunkHeaders: headers})
		counts[file.Kind]++
	}
	for _, commit := range change.Commits {
		subjects = append(subjects, commit.Subject)
	}
	if len(files) == 0 {
		return request{}, false
	}

	r := request{
		state: map[string]any{
			"commits":   change.Commits,
			"files":     files,
			"docs_diff": docsDiff,
			"change": map[string]any{
				"summary": map[string]any{
					"commit_subjects":   subjects,
					"files_by_kind":     counts,
					"added_symbols":     addedSymbols,
					"added_config_keys": addedKeys,
				},
			},
			"items": map[string]any{string(ItemsCommits): change.Commits},
		},
		questions: map[string]wireQuestion{},
		pending:   map[string]pending{},
	}
	code := counts[FileSource] + counts[FileTest] + counts[FileConfig] + counts[FileWorkflow]
	for _, q := range questionsFor(ScopeChange) {
		switch {
		case q.Items == ItemsCommits:
			for i, commit := range change.Commits {
				r.add(q, i, pending{question: q, symbol: commit.SHA})
			}
		case q.ID == "scope_creep":
			if len(files) >= 2 || len(change.Commits) >= 2 {
				r.add(q, -1, pending{question: q})
			}
		case q.ID == "behavior_change_undocumented":
			if code > 0 {
				r.add(q, -1, pending{question: q})
			}
		case q.ID == "describes_unimplemented":
			if docsDiff != "" {
				r.add(q, -1, pending{question: q})
			}
		}
	}
	if err := r.fit(); err != nil {
		r.oversize = err.Error()
	}
	return r, len(r.questions) > 0
}

// add emits one wire question. Item questions are rewritten from `item.` to the
// concrete list element so every answer maps back to one location.
func (r *request) add(q Question, index int, p pending) {
	id := q.ID
	text, inspect := q.Text, q.Inspect
	if index >= 0 {
		id = fmt.Sprintf("%s#%d", q.ID, index)
		element := fmt.Sprintf("items.%s[%d]", q.Items, index)
		text = strings.ReplaceAll(text, "`item.", "`"+element+".")
		if inspect == "item" {
			inspect = element
		} else {
			inspect = strings.Replace(inspect, "item.", element+".", 1)
		}
	}

	instructions := map[string]string{"question": text, "inspect": inspect}
	var criteria any
	if q.Primitive == PrimitiveScore {
		criteria = q.Levels
	} else if q.True != "" || q.False != "" {
		criteria = map[string]string{"true": q.True, "false": q.False}
	}
	r.questions[id] = wireQuestion{Type: q.Primitive, Instructions: instructions, Criteria: criteria}
	r.pending[id] = p
}

// shrinkLimits are the successive per-field character budgets fit applies.
// Each round shortens more aggressively than the last.
var shrinkLimits = []int{maxStateChars / 4, maxStateChars / 16, maxStateChars / 64, 256}

// fit keeps the state inside the model's context by shortening the least
// specific fields first: package signatures, then the declaration text, the
// Markdown diff, the hunk diff, and finally the judged items. Items are only
// shortened, never dropped, because every question already names the element
// it judges. Each round rewrites from the original text, so one field never
// collects two truncation markers.
// summaryLists are top-level lists that describe the change but are not judged
// items, so they can lose entries once every text field has been shortened.
var summaryLists = []string{"files", "commits"}

// summaryTextLists are the change.summary lists: free text that has no other
// cap, so they shorten and then shrink with the summaries.
var summaryTextLists = []string{"commit_subjects", "added_symbols", "added_config_keys"}

func truncateStrings(values []string, limit int) []string {
	short := make([]string, len(values))
	for i, value := range values {
		short[i] = truncate(value, limit)
	}
	return short
}

// fit returns an error when the state is still over the limit after every
// shrink; the caller reports that instead of sending a request the API would
// reject.
func (r *request) fit() error {
	if r.size() <= maxStateChars {
		return nil
	}
	if neighbours, ok := r.state["neighbours"].(map[string]any); ok {
		neighbours["package_functions"] = []string{}
	}
	if r.size() <= maxStateChars {
		return nil
	}

	hunk, _ := r.state["hunk"].(map[string]any)
	after, _ := hunk["after"].(string)
	diff, _ := hunk["diff"].(string)
	docs, _ := r.state["docs_diff"].(string)
	lists, _ := r.state["items"].(map[string]any)
	items := genericItems(lists)
	summaries := map[string]any{}
	for _, key := range summaryLists {
		if value, ok := r.state[key]; ok {
			summaries[key] = value
		}
	}
	change, _ := r.state["change"].(map[string]any)
	summary, _ := change["summary"].(map[string]any)
	summaryTexts := map[string][]string{}
	for _, key := range summaryTextLists {
		if values, ok := summary[key].([]string); ok {
			summaryTexts[key] = values
		}
	}
	for _, limit := range shrinkLimits {
		if hunk != nil {
			hunk["after"] = truncate(after, limit)
		}
		if r.size() <= maxStateChars {
			return nil
		}
		if docs != "" {
			r.state["docs_diff"] = truncate(docs, limit)
		}
		if r.size() <= maxStateChars {
			return nil
		}
		if hunk != nil {
			hunk["diff"] = truncate(diff, limit)
		}
		if r.size() <= maxStateChars {
			return nil
		}
		if items != nil {
			r.state["items"] = truncateItems(lists, items, limit)
		}
		if r.size() <= maxStateChars {
			return nil
		}
		for key, value := range summaries {
			r.state[key] = capList(truncateValue(genericJSON(value), limit), limit/listDivisor)
		}
		for key, values := range summaryTexts {
			summary[key] = capList(truncateStrings(values, limit), limit/listDivisor)
		}
		if r.size() <= maxStateChars {
			return nil
		}
	}
	return fmt.Errorf("state is %d characters after shrinking; the limit is %d", r.size(), maxStateChars)
}

// listDivisor turns a character budget into a list-length budget: a 256-char
// round keeps four entries.
const listDivisor = 64

// genericJSON converts a typed value to its generic JSON shape so truncateValue
// can shorten the strings inside it. Values that do not round-trip are
// returned unchanged.
func genericJSON(value any) any {
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return value
	}
	return generic
}

// capList keeps the first n entries of a JSON list and a count of the rest.
// Values that are not lists are returned unchanged.
func capList(value any, n int) any {
	if n < 1 {
		n = 1
	}
	data, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var list []any
	// The marker replaces at least two entries, so a cap never grows the list.
	if err := json.Unmarshal(data, &list); err != nil || len(list) <= n+1 {
		return value
	}
	return append(list[:n], fmt.Sprintf("... %d more", len(list)-n))
}

// genericItems converts the preselected lists to a generic JSON shape so one
// pass shortens comments, errors, sentences, and commits alike. Every field
// keeps its JSON name, so the wire shape does not change.
func genericItems(lists map[string]any) map[string][]map[string]any {
	if lists == nil {
		return nil
	}

	generic := map[string][]map[string]any{}
	for name, list := range lists {
		data, err := json.Marshal(list)
		if err != nil {
			continue
		}
		var items []map[string]any
		if err := json.Unmarshal(data, &items); err != nil {
			continue
		}
		generic[name] = items
	}
	return generic
}

// truncateItems shortens every text field of every item to limit. Lists that
// genericItems could not convert are kept as they were rather than dropped, so
// a question can never point at a list that vanished.
func truncateItems(original map[string]any, items map[string][]map[string]any, limit int) map[string]any {
	shortened := maps.Clone(original)
	for name, list := range items {
		if len(list) == 0 {
			shortened[name] = list
			continue
		}
		short := make([]map[string]any, 0, len(list))
		for _, item := range list {
			fields := map[string]any{}
			for field, value := range item {
				fields[field] = truncateValue(value, limit)
			}
			short = append(short, fields)
		}
		shortened[name] = short
	}
	return shortened
}

// truncateValue shortens strings and caps nested lists (a commit's files or
// hunk headers) without changing the number of items themselves.
func truncateValue(value any, limit int) any {
	switch typed := value.(type) {
	case string:
		return truncate(typed, limit)
	case map[string]any:
		short := make(map[string]any, len(typed))
		for field, element := range typed {
			short[field] = truncateValue(element, limit)
		}
		return short
	case []any:
		short := make([]any, len(typed))
		for i, element := range typed {
			short[i] = truncateValue(element, limit)
		}
		return capList(short, max(1, limit/listDivisor))
	}
	return value
}

func (r *request) size() int {
	data, _ := json.Marshal(r.state)
	return len(data)
}

// chars is what the request sends: the state and every question.
func (r request) chars() int {
	questions, _ := json.Marshal(r.questions)
	return r.size() + len(questions)
}

// label names what a request judges: a file and symbol, or the change.
func (r request) label() string {
	for _, p := range r.pending {
		if p.path != "" {
			return strings.TrimSpace(p.path + " " + p.symbol)
		}
	}
	return "change"
}

// label names one judged location, as path:line symbol question. A commit
// question names its commit, and the other change questions name the change.
func (p pending) label() string {
	location := p.path
	if p.line > 0 {
		location = fmt.Sprintf("%s:%d", location, p.line)
	}
	switch {
	case p.path != "" && p.symbol != "":
		location += " " + p.symbol
	case p.symbol != "":
		location = "commit " + p.symbol
	case location == "":
		location = "change"
	}
	return location + " " + p.question.ID
}

func compose(report *Report, r request, answers map[string]Answer) {
	ids := make([]string, 0, len(r.pending))
	for id := range r.pending {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		p := r.pending[id]
		answer, ok := answers[id]
		value, confidence, valid := answerValue(p.question, answer)
		if !ok || !valid {
			report.Missing = append(report.Missing, p.label())
			continue
		}

		var fired bool
		if p.question.Direction == DirectionLow {
			fired = value <= p.question.Threshold
		} else {
			fired = value >= p.question.Threshold
		}
		uncertain := p.question.Primitive == PrimitiveScore && (confidence == nil || *confidence < p.question.ConfidenceMin)
		if uncertain {
			fired = false
		}

		report.Judgments = append(report.Judgments, Judgment{Question: p.question.ID, Path: p.path, Line: p.line, Symbol: p.symbol, Value: value, Confidence: confidence, Fired: fired, Uncertain: uncertain})
		if fired {
			report.Findings = append(report.Findings, Finding{
				Question:   p.question.ID,
				Severity:   p.question.Severity,
				Path:       p.path,
				Line:       p.line,
				Symbol:     p.symbol,
				Value:      value,
				Confidence: confidence,
				Threshold:  p.question.Threshold,
				Message:    p.question.Issue,
			})
		}
	}
}

func answerValue(q Question, answer Answer) (float64, *float64, bool) {
	if q.Primitive == PrimitiveScore {
		if answer.Score == nil {
			return 0, nil, false
		}
		return *answer.Score, answer.Confidence, true
	}
	if answer.Noul == nil {
		return 0, nil, false
	}
	return *answer.Noul, nil, true
}

var severityOrder = map[Severity]int{
	SeverityImportant: 0,
	SeverityMinor:     1,
	SeverityNit:       2,
}

func severityRank(severity Severity) int {
	if rank, ok := severityOrder[severity]; ok {
		return rank
	}
	return len(severityOrder)
}

// Summary renders the report for a human reading check output.
func Summary(report Report) string {
	var b strings.Builder
	uncertain := 0
	for _, j := range report.Judgments {
		if j.Uncertain {
			uncertain++
		}
	}
	fmt.Fprintf(&b, "semantic-lint (%s): %d findings, %d uncertain, %d judgments, %d requests, %d input tokens, model %s\n",
		report.Mode, len(report.Findings), uncertain, len(report.Judgments), report.Requests, report.InputTokens, firstNonEmpty(report.ServedModel, report.Model))
	for _, f := range report.Findings {
		location := f.Path
		if f.Line > 0 {
			location = fmt.Sprintf("%s:%d", f.Path, f.Line)
		}
		if f.Symbol != "" {
			location += " (" + f.Symbol + ")"
		}
		fmt.Fprintf(&b, "  %-9s %-30s %s  %.2f vs %.2f  %s\n", f.Severity, f.Question, strings.TrimSpace(location), f.Value, f.Threshold, f.Message)
	}
	for _, label := range report.Missing {
		fmt.Fprintf(&b, "  unanswered %s\n", label)
	}
	for _, reason := range report.Errors {
		fmt.Fprintf(&b, "  request failed: %s\n", reason)
	}
	for _, note := range report.Notes {
		fmt.Fprintf(&b, "  note: %s\n", note)
	}
	return b.String()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// keep queues a request, or records why an oversize one is not sent so the
// report names the skipped element instead of failing on an API rejection.
func keep(requests []request, notes []string, r request) ([]request, []string) {
	if r.oversize == "" {
		return append(requests, r), notes
	}
	return requests, append(notes, fmt.Sprintf("%s was not judged: %s", r.label(), r.oversize))
}
