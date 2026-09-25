package semantic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/wangjohn/levenshtein/internal/testgit"
)

func TestParseDiffZeroContext(t *testing.T) {
	raw := strings.Join([]string{
		"diff --git a/pkg/a.go b/pkg/a.go",
		"index 1111111..2222222 100644",
		"--- a/pkg/a.go",
		"+++ b/pkg/a.go",
		"@@ -3 +3,2 @@ func old() {",
		"-\told := 1",
		"+\tfresh := 1",
		"+\t_ = fresh",
		"@@ -10,0 +12 @@ func other() {",
		"+\t// trailing",
		"diff --git a/docs/new.md b/docs/new.md",
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/docs/new.md",
		"@@ -0,0 +1,2 @@",
		"+# Title",
		"+Body.",
		"diff --git a/img.png b/img.png",
		"Binary files a/img.png and b/img.png differ",
		"diff --git a/gone.go b/gone.go",
		"deleted file mode 100644",
		"--- a/gone.go",
		"+++ /dev/null",
		"@@ -1 +0,0 @@",
		"-package gone",
	}, "\n")

	files, err := parseDiff(raw, func(string) bool { return true })
	if err != nil || len(files) != 3 {
		t.Fatalf("files: %+v", files)
	}

	first := files[0]
	if first.Path != "pkg/a.go" || first.Kind != FileSource || first.Status != FileModified || len(first.Hunks) != 2 || first.Added != 3 || first.Removed != 1 {
		t.Fatalf("modified file: %+v", first)
	}
	if h := first.Hunks[0]; h.OldStart != 3 || h.OldLines != 1 || h.NewStart != 3 || h.NewLines != 2 || h.Header != "func old() {" || len(h.Added) != 2 {
		t.Fatalf("first hunk: %+v", h)
	}
	if h := first.Hunks[1]; h.OldLines != 0 || h.NewStart != 12 || h.NewLines != 1 {
		t.Fatalf("second hunk: %+v", h)
	}

	if docs := files[1]; docs.Path != "docs/new.md" || docs.Kind != FileDocs || docs.Status != FileAdded || docs.Hunks[0].NewLines != 2 {
		t.Fatalf("added docs: %+v", docs)
	}
	if gone := files[2]; gone.Path != "gone.go" || gone.Status != FileDeleted {
		t.Fatalf("deleted file: %+v", gone)
	}
}

// Git ends a name containing a space with a tab, quotes one with a quote even
// with core.quotePath off, and with it on escapes non-ASCII bytes in octal.
var awkwardHeaders = []struct {
	header string
	path   string
}{
	{"a/my file.go\t", "my file.go"},
	{`"a/q\"uote.go"`, `q"uote.go`},
	{`"a/caf\303\251.go"`, "café.go"},
	{"a/café.go", "café.go"},
}

func TestParseDiffReadsAwkwardPaths(t *testing.T) {
	for _, tc := range awkwardHeaders {
		newSide := strings.Replace(tc.header, "a/", "b/", 1)
		raw := strings.Join([]string{
			"diff --git " + tc.header + " " + newSide,
			"--- " + tc.header,
			"+++ " + newSide,
			"@@ -1 +1 @@",
			"-old",
			"+new",
		}, "\n")

		files, err := parseDiff(raw, func(string) bool { return true })

		if err != nil || len(files) != 1 || files[0].Path != tc.path || files[0].Kind != FileSource {
			t.Errorf("%s: %+v %v", tc.header, files, err)
		}
	}

	deleted := "diff --git \"a/q\\\"uote.go\" \"b/q\\\"uote.go\"\ndeleted file mode 100644\n--- \"a/q\\\"uote.go\"\n+++ /dev/null\n@@ -1 +0,0 @@\n-package q\n"
	if files, err := parseDiff(deleted, func(string) bool { return true }); err != nil || len(files) != 1 || files[0].Path != `q"uote.go` {
		t.Errorf("a deleted file is named by its old side: %+v %v", files, err)
	}
}

func TestParseCommitLogReadsAwkwardPaths(t *testing.T) {
	for _, tc := range awkwardHeaders {
		newSide := strings.Replace(tc.header, "a/", "b/", 1)
		raw := strings.Join([]string{
			commitRecord + "0123456789abcdef\x1fSubject",
			"diff --git " + tc.header + " " + newSide,
			"--- " + tc.header,
			"+++ " + newSide,
			"@@ -1 +1 @@",
			"-old",
			"+new",
		}, "\n")

		commits, err := parseCommitLog(raw)

		if err != nil || len(commits) != 1 || len(commits[0].Files) != 1 || commits[0].Files[0] != tc.path {
			t.Errorf("%s: %+v %v", tc.header, commits, err)
		}
	}
}

func TestLoadChangeReviewsFilesWithAwkwardNames(t *testing.T) {
	names := []string{"pkg/my file.go", `pkg/q"uote.go`, "pkg/café.go"}
	for _, quotePath := range []string{"true", "false"} {
		t.Run("core.quotePath="+quotePath, func(t *testing.T) {
			r := newRepo(t)
			r.run(t, "config", "core.quotePath", quotePath)
			for _, name := range names {
				r.write(t, name, "package pkg\n")
			}
			r.run(t, "add", ".")
			r.run(t, "commit", "--quiet", "-m", "base")
			r.run(t, "switch", "--quiet", "-c", "feature")
			for _, name := range names {
				r.write(t, name, "package pkg\n\n// Added explains nothing.\nvar Added = 1\n")
			}
			r.run(t, "commit", "--quiet", "-am", "Edit awkward names")

			change, err := loadChange(t.Context(), gitRunner{Git: r.git, Dir: r.dir, Env: r.env}, "main", func(string) bool { return true })
			if err != nil {
				t.Fatal(err)
			}

			kinds := map[string]FileKind{}
			for _, file := range change.Files {
				kinds[file.Path] = file.Kind
			}
			if len(change.Commits) != 1 {
				t.Fatalf("commits: %+v", change.Commits)
			}
			for _, name := range names {
				if kinds[name] != FileSource {
					t.Errorf("%s was not reviewed as Go: %v", name, kinds)
				}
				if !slices.Contains(change.Commits[0].Files, name) {
					t.Errorf("%s missing from the commit's files: %v", name, change.Commits[0].Files)
				}
			}
		})
	}
}

const sampleSource = `package sample

import (
	"errors"
	"fmt"
)

// Existing documents an existing helper.
func Existing(path string) error {
	if path == "" {
		return errors.New("empty path")
	}
	return nil
}

// Fresh documents the new function.
func Fresh(path string, strict bool) error {
	// Freshness concerns verification, not reusable preparation.
	if strict && path == "" {
		return fmt.Errorf("path %q must not be empty", path)
	}
	return nil // trailing note
}
`

// lineOf is the line of sampleSource that contains needle.
func lineOf(needle string) int {
	for i, line := range strings.Split(sampleSource, "\n") {
		if strings.Contains(line, needle) {
			return i + 1
		}
	}
	return 0
}

func TestGoUnitsSelectItemsFromAddedLines(t *testing.T) {
	start := lineOf("// Fresh documents")
	end := lineOf("return nil // trailing note") + 1
	hunks := []Hunk{{NewStart: start, NewLines: end - start + 1, Diff: "@@ fresh @@\n"}}

	units := goUnits("sample.go", []byte(sampleSource), hunks)
	if len(units) != 1 {
		t.Fatalf("units: %+v", units)
	}

	unit := units[0]
	if unit.Symbol != "Fresh" || !unit.New || unit.Line != start || !strings.Contains(unit.After, "func Fresh") {
		t.Fatalf("unit identity: %+v", unit)
	}
	if len(unit.Comments) != 2 || !strings.Contains(unit.Comments[0].Comment, "Freshness concerns") || !strings.Contains(unit.Comments[0].Code, "if strict") || unit.Comments[1].Comment != "trailing note" || !strings.Contains(unit.Comments[1].Code, "return nil") {
		t.Fatalf("comments (doc comment must be excluded): %+v", unit.Comments)
	}
	if len(unit.Errors) != 1 || !strings.Contains(unit.Errors[0].Call, "fmt.Errorf") || unit.Errors[0].Function != "Fresh" {
		t.Fatalf("errors: %+v", unit.Errors)
	}

	untouched := goUnits("sample.go", []byte(sampleSource), []Hunk{{NewStart: lineOf(`return errors.New`), NewLines: 1}})
	if len(untouched) != 1 || untouched[0].Symbol != "Existing" || untouched[0].New || !untouched[0].IsFunc {
		t.Fatalf("edited existing function must not count as new: %+v", untouched)
	}
}

func TestPackageFunctionsExcludeEditedSymbols(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample.go"), []byte(sampleSource), 0600); err != nil {
		t.Fatal(err)
	}

	signatures := packageFunctions(dir, filepath.Join(dir, "sample.go"), false, map[string]bool{"Fresh": true})
	if len(signatures) != 1 || !strings.HasPrefix(signatures[0], "func Existing(path string) error") || !strings.Contains(signatures[0], "// Existing documents") {
		t.Fatalf("signatures: %q", signatures)
	}
}

func TestDocUnitsFindAbsoluteSentencesOutsideCode(t *testing.T) {
	doc := "# Guide\n\nIntro text.\n\n## Cache\n\nResults are never reused across hosts. Restarts are cheap.\n\n```sh\nnever run this\n```\n\n| never | table |\n"
	lines := strings.Split(doc, "\n")
	hunk := Hunk{NewStart: 7, NewLines: 7, Added: lines[6:13], Diff: "@@ -7,0 +7,7 @@\n"}

	units := docUnits("docs/guide.md", []byte(doc), []Hunk{hunk})
	if len(units) != 1 || !units[0].Prose || len(units[0].Sentences) != 1 {
		t.Fatalf("units: %+v", units)
	}
	if s := units[0].Sentences[0]; s.Sentence != "Results are never reused across hosts." || s.Line != 7 {
		t.Fatalf("sentence: %+v", s)
	}
	if !strings.HasPrefix(units[0].After, "## Cache") || strings.Contains(units[0].After, "Intro text") {
		t.Fatalf("section: %q", units[0].After)
	}
}

func TestTruncationCutsOnRuneBoundaries(t *testing.T) {
	text := strings.Repeat("é", 50) // Two bytes per rune, so an odd limit splits one.
	for _, limit := range []int{1, 7, 31, 99} {
		short := truncate(text, limit)
		if !utf8.ValidString(short) || len(short) > limit+len("\n... [truncated]") {
			t.Fatalf("limit %d: %q", limit, short)
		}
	}

	body := []byte(strings.Repeat("€", 200)) // Three bytes per rune, so byte 400 lands inside one.
	if short := summary(body); !utf8.ValidString(short) || !strings.HasSuffix(short, "...") {
		t.Fatalf("error body: %q", short)
	}
	if short := truncate("plain", 99); short != "plain" {
		t.Fatalf("text within the limit must be untouched: %q", short)
	}
}

// TestFitShrinksAnOversizedDeclaration covers the state no earlier step could
// shrink: one new declaration whose diff and comments together dwarf the cap.
func TestFitShrinksAnOversizedDeclaration(t *testing.T) {
	var src strings.Builder
	src.WriteString("package sample\n\n// Huge is one declaration larger than the whole state cap.\nfunc Huge() {\n")
	for i := range 40 {
		fmt.Fprintf(&src, "\t// %s\n\t_ = %d\n", strings.Repeat("the retry window stays short ", 200), i)
	}
	for i := range 4000 {
		fmt.Fprintf(&src, "\t_ = %d\n", i)
	}
	src.WriteString("}\n")

	text := src.String()
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	diff := fmt.Sprintf("@@ -0,0 +1,%d @@\n+%s\n", len(lines), strings.Join(lines, "\n+"))
	units := goUnits("pkg/huge.go", []byte(text), []Hunk{{NewStart: 1, NewLines: len(lines), Added: lines, Diff: diff}})
	if len(units) != 1 || len(units[0].Comments) != 40 {
		t.Fatalf("units: %+v", units)
	}

	r, ok := goRequest(units[0], false, nil)
	if !ok || len(r.questions) != len(r.pending) || len(r.questions) < 40 {
		t.Fatalf("questions: %d pending %d", len(r.questions), len(r.pending))
	}
	if size := r.size(); size > maxStateChars {
		t.Fatalf("state stayed oversized: %d chars", size)
	}

	encoded, err := json.Marshal(r.state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "[truncated]") {
		t.Fatal("shortened fields must carry a visible marker")
	}
}

// fakeJev answers every noul with one value and every score with another,
// optionally dropping one question to simulate an incomplete response.
type fakeJev struct {
	noul     float64
	score    float64
	drop     string
	calls    atomic.Int32
	mu       sync.Mutex
	statuses []int
	seen     []string
}

func (f *fakeJev) handler(t *testing.T) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		call := int(f.calls.Add(1)) - 1
		if r.Header.Get("Authorization") != "Bearer test-key" || r.URL.Path != "/v1/systemone" {
			t.Errorf("request shape: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		if call < len(f.statuses) && f.statuses[call] != http.StatusOK {
			w.WriteHeader(f.statuses[call])
			return
		}

		var req wireRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if req.Model != DefaultModel {
			t.Errorf("model: %q", req.Model)
		}
		answers := map[string]Answer{}
		f.mu.Lock()
		defer f.mu.Unlock()
		for id, q := range req.Questions {
			f.seen = append(f.seen, id)
			if id == f.drop {
				continue
			}
			if q.Type == PrimitiveScore {
				score, confidence := f.score, 0.9
				answers[id] = Answer{Type: PrimitiveScore, Score: &score, Confidence: &confidence}
				continue
			}
			noul := f.noul
			answers[id] = Answer{Type: PrimitiveNoul, Noul: &noul}
		}
		_ = json.NewEncoder(w).Encode(wireResponse{Model: DefaultModel, Answers: answers, Usage: wireUsage{InputTokens: 100}})
	}
}

type repo struct {
	dir string
	git string
	env []string
}

func newRepo(t *testing.T) repo {
	t.Helper()
	r := repo{dir: t.TempDir(), git: testgit.Path(t), env: testgit.Env()}
	r.run(t, "init", "--quiet", "--initial-branch=main")
	return r
}

func (r repo) run(t *testing.T, args ...string) {
	t.Helper()
	testgit.Run(t, r.git, r.dir, args...)
}

func (r repo) write(t *testing.T, path, content string) {
	t.Helper()
	full := filepath.Join(r.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

// changedRepo commits a base on main, then a branch that adds a Go function,
// edits documentation with an absolute claim, and leaves one untracked file.
func changedRepo(t *testing.T) repo {
	t.Helper()
	r := newRepo(t)
	r.write(t, "pkg/sample.go", strings.Replace(sampleSource, "// Fresh documents the new function.\nfunc Fresh(path string, strict bool) error {\n\t// Freshness concerns verification, not reusable preparation.\n\tif strict && path == \"\" {\n\t\treturn fmt.Errorf(\"path %q must not be empty\", path)\n\t}\n\treturn nil // trailing note\n}\n", "var _ = fmt.Sprint\n", 1))
	r.write(t, "docs/guide.md", "# Guide\n\nResults are reused when inputs match.\n")
	r.write(t, "AGENTS.md", "# Agent preferences\n\n- Keep related statements together.\n")
	r.run(t, "add", ".")
	r.run(t, "commit", "--quiet", "-m", "Initial layout")

	r.run(t, "switch", "--quiet", "-c", "feature")
	r.write(t, "pkg/sample.go", sampleSource)
	r.write(t, "docs/guide.md", "# Guide\n\nResults are reused when inputs match. Cached results never go stale.\n")
	r.run(t, "add", ".")
	r.run(t, "commit", "--quiet", "-m", "Add strict path validation")
	r.write(t, "pkg/untracked.go", "package sample\n\n// Untracked is new and not yet added.\nfunc Untracked() {}\n")
	return r
}

func runOptions(r repo, server *httptest.Server) Options {
	return Options{
		Source:  r.dir,
		Include: func(string) bool { return true },
		Git:     r.git,
		Env:     r.env,
		Base:    "main",
		Client:  Client{BaseURL: server.URL, APIKey: "test-key", Model: DefaultModel},
	}
}

func TestRunComposesAdvisoryFindings(t *testing.T) {
	r := changedRepo(t)
	jev := &fakeJev{noul: 0.95, score: 2}
	server := httptest.NewServer(jev.handler(t))
	defer server.Close()

	report, err := Run(context.Background(), runOptions(r, server))
	if err != nil {
		t.Fatal(err)
	}
	if report.Mode != ModeAdvisory || report.BaseRef != "main" || report.Base == "" || report.ServedModel != DefaultModel || len(report.Missing) != 0 {
		t.Fatalf("report header: %+v", report)
	}
	if report.Requests < 3 || report.InputTokens != 100*report.Requests {
		t.Fatalf("requests: %+v", report)
	}

	fired := map[string][]Finding{}
	for _, f := range report.Findings {
		fired[f.Question] = append(fired[f.Question], f)
	}
	if len(fired["comment_explains_why"]) != 0 || len(fired["error_message_actionable"]) != 0 || len(fired["commit_subject_matches"]) != 0 {
		t.Fatalf("positive-phrased questions fired on a high probability: %+v", report.Findings)
	}
	if guarantee := fired["unscoped_guarantee"]; len(guarantee) != 1 || guarantee[0].Path != "docs/guide.md" || guarantee[0].Line != 3 {
		t.Fatalf("absolute doc claim: %+v", report.Findings)
	}
	if creep := fired["scope_creep"]; len(creep) != 1 || creep[0].Confidence == nil {
		t.Fatalf("scored change question must carry confidence: %+v", fired["scope_creep"])
	}
	if len(fired["behavior_change_undocumented"]) != 1 || len(fired["describes_unimplemented"]) != 1 {
		t.Fatalf("change-scope and hunk questions: %+v", report.Findings)
	}
	if report.Findings[0].Severity != SeverityImportant {
		t.Fatalf("findings must be ordered by severity: %+v", report.Findings)
	}

	judged := map[string]bool{}
	for _, j := range report.Judgments {
		judged[j.Question] = true
	}
	for _, id := range []string{"comment_explains_why", "error_message_actionable", "commit_subject_matches", "duplicates_package_helper"} {
		if !judged[id] {
			t.Errorf("no judgment recorded for %s: %+v", id, report.Judgments)
		}
	}

	summary := Summary(report)
	if !strings.HasPrefix(summary, "semantic-lint (advisory): ") || !strings.Contains(summary, "unscoped_guarantee") {
		t.Fatalf("summary: %s", summary)
	}
}

func TestRunReportsUnansweredQuestions(t *testing.T) {
	r := changedRepo(t)
	jev := &fakeJev{noul: 0.1, score: 0, drop: "scope_creep"}
	server := httptest.NewServer(jev.handler(t))
	defer server.Close()

	report, err := Run(context.Background(), runOptions(r, server))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Missing) != 1 || report.Missing[0] != "change scope_creep" {
		t.Fatalf("missing: %+v", report.Missing)
	}
	highDirection := map[string]bool{"scope_creep": true, "unscoped_guarantee": true, "duplicates_package_helper": true}
	for _, f := range report.Findings {
		if highDirection[f.Question] {
			t.Fatalf("low probability fired a high-direction question: %+v", f)
		}
	}
	if len(report.Findings) == 0 {
		t.Fatal("low probabilities must fire the positive-phrased questions")
	}
}

// Wire ids such as comment_explains_why#0 repeat in every request, so the
// report names the location and symbol each unanswered question judged.
func TestRunNamesWhereAnUnansweredItemIs(t *testing.T) {
	r := changedRepo(t)
	r.write(t, "pkg/other.go", "package sample\n\nfunc Other() {\n\t// Other keeps a comment of its own.\n\t_ = 1\n}\n")
	jev := &fakeJev{noul: 0.9, score: 0, drop: "comment_explains_why#0"}
	server := httptest.NewServer(jev.handler(t))
	defer server.Close()

	report, err := Run(t.Context(), runOptions(r, server))
	if err != nil {
		t.Fatal(err)
	}

	freshness := fmt.Sprintf("pkg/sample.go:%d Fresh comment_explains_why", lineOf("// Freshness concerns"))
	want := []string{"pkg/other.go:4 Other comment_explains_why", freshness}
	if !slices.Equal(report.Missing, want) {
		t.Fatalf("missing = %q, want %q", report.Missing, want)
	}
	if summary := Summary(report); !strings.Contains(summary, "unanswered "+freshness) {
		t.Fatalf("summary must name the location: %s", summary)
	}
}

// labelledJev answers like fakeJev, but rejects any request whose state is for
// a path in reject, and stalls on one in stall until the request is abandoned.
func labelledJev(t *testing.T, reject, stall string, release <-chan struct{}) (http.HandlerFunc, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	return func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var req wireRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		state, _ := req.State.(map[string]any)
		file, _ := state["file"].(map[string]any)
		path, _ := file["path"].(string)
		switch path {
		case "":
		case reject:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"state too large"}`))
			return
		case stall:
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		answers := map[string]Answer{}
		for id, q := range req.Questions {
			value := 0.9
			if q.Type == PrimitiveScore {
				answers[id] = Answer{Type: PrimitiveScore, Score: &value, Confidence: &value}
				continue
			}
			answers[id] = Answer{Type: PrimitiveNoul, Noul: &value}
		}
		_ = json.NewEncoder(w).Encode(wireResponse{Model: DefaultModel, Answers: answers, Usage: wireUsage{InputTokens: 10}})
	}, &calls
}

func TestRunKeepsTheAnswersOfRequestsThatSucceeded(t *testing.T) {
	r := changedRepo(t)
	handler, _ := labelledJev(t, "pkg/sample.go", "", nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	report, err := Run(t.Context(), runOptions(r, server))
	if err != nil {
		t.Fatalf("one rejected request must not discard the rest: %v", err)
	}

	if len(report.Judgments) == 0 || len(report.Missing) == 0 {
		t.Fatalf("judgments %d, missing %v", len(report.Judgments), report.Missing)
	}
	for _, missing := range report.Missing {
		if !strings.HasPrefix(missing, "pkg/sample.go:") {
			t.Errorf("only the rejected file's questions are unanswered: %v", report.Missing)
		}
	}
	if len(report.Errors) != 1 || !strings.Contains(report.Errors[0], "HTTP 400") {
		t.Fatalf("errors = %q", report.Errors)
	}
}

func TestRunReturnsPartialResultsWhenItsDeadlinePasses(t *testing.T) {
	r := changedRepo(t)
	release := make(chan struct{})
	handler, _ := labelledJev(t, "", "pkg/sample.go", release)
	server := httptest.NewServer(handler)
	defer server.Close()
	defer close(release)
	opts := runOptions(r, server)
	opts.Concurrency = 1

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	report, err := Run(ctx, opts)
	if err != nil {
		t.Fatalf("a deadline mid-run must keep what was answered: %v", err)
	}

	if len(report.Judgments) == 0 {
		t.Fatal("answers received before the deadline were discarded")
	}
	stalled := false
	for _, missing := range report.Missing {
		stalled = stalled || strings.HasPrefix(missing, "pkg/sample.go")
	}
	if !stalled || len(report.Errors) == 0 || !strings.Contains(strings.Join(report.Errors, " "), "timed out") {
		t.Fatalf("missing %v, errors %q", report.Missing, report.Errors)
	}
}

func TestRunStaysWithinItsBudgets(t *testing.T) {
	r := changedRepo(t)
	handler, calls := labelledJev(t, "", "", nil)
	server := httptest.NewServer(handler)
	defer server.Close()

	opts := runOptions(r, server)
	opts.MaxRequests = 1
	report, err := Run(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}

	if calls.Load() != 1 || report.Requests != 1 {
		t.Fatalf("max_requests 1 sent %d requests", calls.Load())
	}
	if len(report.Skipped) == 0 || len(report.Missing) != 0 {
		t.Fatalf("requests over the budget are skipped, not unanswered: skipped %v missing %v", report.Skipped, report.Missing)
	}
	if !strings.Contains(strings.Join(report.Notes, " "), "max_requests") {
		t.Fatalf("notes must name the budget: %q", report.Notes)
	}
	// The change-level request is sent first, so a tight budget still judges
	// the change as a whole.
	for _, j := range report.Judgments {
		if j.Path != "" {
			t.Fatalf("the change-level request must win the only slot: %+v", j)
		}
	}

	opts.MaxRequests = 0
	opts.MaxInputChars = 1
	calls.Store(0)
	report, err = Run(t.Context(), opts)
	if err != nil || calls.Load() != 0 || len(report.Skipped) == 0 || !strings.Contains(strings.Join(report.Notes, " "), "max_input_chars") {
		t.Fatalf("an input budget below every request sends none: calls %d %+v %v", calls.Load(), report, err)
	}
}

func TestRunWithoutChangesNeedsNoModel(t *testing.T) {
	r := newRepo(t)
	r.write(t, "README.md", "# Empty\n")
	r.run(t, "add", ".")
	r.run(t, "commit", "--quiet", "-m", "Initial")
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("model called without changes") }))
	defer server.Close()

	report, err := Run(context.Background(), runOptions(r, server))
	if err != nil || report.Requests != 0 || len(report.Notes) != 1 {
		t.Fatalf("empty change: %+v %v", report, err)
	}

	_, err = Run(context.Background(), Options{Source: r.dir, Include: func(string) bool { return true }, Git: r.git, Env: r.env, Base: "nonexistent", Client: Client{BaseURL: server.URL, APIKey: "test-key", Model: DefaultModel}})
	if err == nil || !strings.Contains(err.Error(), "nonexistent") {
		t.Fatalf("missing base must be explicit: %v", err)
	}
}

func TestLoadChangePinsDiffPrefixes(t *testing.T) {
	r := changedRepo(t)
	// Mnemonic prefixes would label the sides c/ and w/ instead of a/ and b/.
	r.run(t, "config", "diff.mnemonicPrefix", "true")

	change, err := loadChange(context.Background(), gitRunner{Git: r.git, Dir: r.dir, Env: r.env}, "main", func(string) bool { return true })
	if err != nil {
		t.Fatal(err)
	}

	paths := map[string]bool{}
	for _, file := range change.Files {
		paths[file.Path] = true
	}
	for _, commit := range change.Commits {
		for _, path := range commit.Files {
			paths[path] = true
		}
	}
	for _, want := range []string{"pkg/sample.go", "docs/guide.md"} {
		if !paths[want] {
			t.Fatalf("prefixes leaked into paths: %v", paths)
		}
	}
}

func TestCommitsReadEveryCommitInOnePass(t *testing.T) {
	r := changedRepo(t)
	r.write(t, "pkg/extra.go", "package sample\n\n// Extra is a second commit.\nfunc Extra() {}\n")
	r.run(t, "add", "pkg/extra.go")
	r.run(t, "commit", "--quiet", "-m", "Add an extra helper")

	g := gitRunner{Git: r.git, Dir: r.dir, Env: r.env}
	_, mergeBase, err := g.ResolveBase(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	commits, err := loadCommits(context.Background(), g, mergeBase)
	if err != nil {
		t.Fatal(err)
	}

	if len(commits) != 2 || commits[0].Subject != "Add an extra helper" || commits[1].Subject != "Add strict path validation" {
		t.Fatalf("subjects, newest first: %+v", commits)
	}
	if len(commits[0].SHA) != 12 || len(commits[0].Files) != 1 || commits[0].Files[0] != "pkg/extra.go" {
		t.Fatalf("files of the newest commit: %+v", commits[0])
	}
	if len(commits[0].HunkHeaders) != 1 || !strings.HasPrefix(commits[0].HunkHeaders[0], "pkg/extra.go @@ -0,0 +1,4 @@") {
		t.Fatalf("hunk headers: %+v", commits[0].HunkHeaders)
	}
	if len(commits[1].Files) != 2 || len(commits[1].HunkHeaders) == 0 {
		t.Fatalf("older commit: %+v", commits[1])
	}
}

func TestShallowCloneGetsFetchDepthAdvice(t *testing.T) {
	r := changedRepo(t)
	r.run(t, "switch", "--quiet", "main")
	clone := repo{dir: t.TempDir(), git: r.git, env: r.env}
	clone.run(t, "clone", "--quiet", "--depth", "1", "--branch", "feature", "file://"+r.dir, clone.dir)

	_, _, err := gitRunner{Git: clone.git, Dir: clone.dir, Env: clone.env}.ResolveBase(context.Background(), "main")
	if err == nil || !strings.Contains(err.Error(), "fetch-depth: 0") {
		t.Fatalf("shallow clone advice: %v", err)
	}
}

func TestClientRetriesRateLimitsButNotRejections(t *testing.T) {
	jev := &fakeJev{noul: 0.5, statuses: []int{http.StatusServiceUnavailable, http.StatusTooManyRequests, http.StatusOK}}
	server := httptest.NewServer(jev.handler(t))
	defer server.Close()
	client := Client{BaseURL: server.URL, APIKey: "test-key", Model: DefaultModel, sleep: recordSleeps(new([]time.Duration))}
	questions := map[string]wireQuestion{"q": {Type: PrimitiveNoul, Instructions: "x"}}

	response, err := client.Ask(context.Background(), "state", questions)
	if err != nil || jev.calls.Load() != 3 || response.Answers["q"].Noul == nil {
		t.Fatalf("retry: %+v %v calls=%d", response, err, jev.calls.Load())
	}

	rejecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer rejecting.Close()
	client.BaseURL = rejecting.URL

	var apiErr *APIError
	if _, err := client.Ask(context.Background(), "state", questions); !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("rejection must not retry: %v", err)
	}
}

// recordSleeps stands in for the retry wait and records each delay.
func recordSleeps(delays *[]time.Duration) func(context.Context, time.Duration) error {
	return func(_ context.Context, delay time.Duration) error {
		*delays = append(*delays, delay)
		return nil
	}
}

func TestClientDoesNotWaitAfterTheLastAttempt(t *testing.T) {
	jev := &fakeJev{statuses: []int{http.StatusServiceUnavailable, http.StatusServiceUnavailable, http.StatusServiceUnavailable}}
	server := httptest.NewServer(jev.handler(t))
	defer server.Close()
	var delays []time.Duration
	client := Client{BaseURL: server.URL, APIKey: "test-key", Model: DefaultModel, sleep: recordSleeps(&delays)}

	_, err := client.Ask(t.Context(), "state", map[string]wireQuestion{"q": {Type: PrimitiveNoul}})

	if err == nil || !strings.Contains(err.Error(), "gave up after 3 attempts") || jev.calls.Load() != 3 {
		t.Fatalf("err=%v calls=%d", err, jev.calls.Load())
	}
	if len(delays) != 2 {
		t.Fatalf("waits = %v, want one between each pair of attempts and none after the last", delays)
	}
}

func TestClientRetriesServerErrors(t *testing.T) {
	jev := &fakeJev{noul: 0.5, statuses: []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusOK}}
	server := httptest.NewServer(jev.handler(t))
	defer server.Close()
	var delays []time.Duration
	client := Client{BaseURL: server.URL, APIKey: "test-key", Model: DefaultModel, sleep: recordSleeps(&delays)}

	response, err := client.Ask(t.Context(), "state", map[string]wireQuestion{"q": {Type: PrimitiveNoul}})

	if err != nil || response.Answers["q"].Noul == nil || jev.calls.Load() != 3 {
		t.Fatalf("a 500 must be retried: %+v %v calls=%d", response, err, jev.calls.Load())
	}
}

// A connection that accepts the request and never answers costs one attempt,
// not the whole run's deadline.
func TestClientRetriesAStalledAttempt(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			select {
			case <-r.Context().Done():
			case <-release:
			}
			return
		}
		score := 0.5
		_ = json.NewEncoder(w).Encode(wireResponse{Model: DefaultModel, Answers: map[string]Answer{"q": {Type: PrimitiveNoul, Noul: &score}}})
	}))
	defer server.Close()
	defer close(release)
	var delays []time.Duration
	client := Client{BaseURL: server.URL, APIKey: "test-key", Model: DefaultModel, AttemptTimeout: 100 * time.Millisecond, sleep: recordSleeps(&delays)}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	response, err := client.Ask(ctx, "state", map[string]wireQuestion{"q": {Type: PrimitiveNoul}})

	if err != nil || response.Answers["q"].Noul == nil || calls.Load() != 2 {
		t.Fatalf("a stalled attempt must be retried: %+v %v calls=%d", response, err, calls.Load())
	}
}

func TestCatalogIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, q := range Catalog {
		if seen[q.ID] {
			t.Fatalf("duplicate id %s", q.ID)
		}
		seen[q.ID] = true
		if q.Text == "" || q.Inspect == "" || q.Issue == "" || q.Threshold <= 0 {
			t.Fatalf("incomplete question: %+v", q)
		}
		switch q.Primitive {
		case PrimitiveNoul:
			if q.True == "" || q.False == "" || len(q.Levels) != 0 || q.Threshold > 1 {
				t.Fatalf("noul criteria: %+v", q)
			}
		case PrimitiveScore:
			if len(q.Levels) < 2 || q.ConfidenceMin <= 0 || q.Threshold > float64(len(q.Levels)-1) {
				t.Fatalf("score rubric: %+v", q)
			}
		}
		if q.Items != ItemsNone && !strings.Contains(q.Text, "`item.") {
			t.Fatalf("item question must reference item fields: %+v", q)
		}
	}
	if len(catalogByID) != len(Catalog) {
		t.Fatal("index drifted")
	}
}
