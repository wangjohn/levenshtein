package semantic

import (
	"fmt"
	"strings"
	"testing"
)

// A content line that begins with "++ " reaches the parser as "+++ ..." and
// must stay content: the file keeps its path and the line stays counted.
func TestDiffContentCannotForgeAFileHeader(t *testing.T) {
	raw := strings.Join([]string{
		"diff --git a/docs/guide.md b/docs/guide.md",
		"new file mode 100644",
		"--- /dev/null",
		"+++ b/docs/guide.md",
		"@@ -0,0 +1,3 @@",
		"+intro",
		"+++ b/pkg/other.go",
		"+-- trailer",
		"",
	}, "\n")

	files, err := parseDiff(raw, func(string) bool { return true })
	if err != nil || len(files) != 1 || files[0].Path != "docs/guide.md" || files[0].Added != 3 {
		t.Fatalf("forged header: %+v %v", files, err)
	}
}

// One over-long line must surface as an error rather than silently ending
// the parse and dropping every later commit or file.
func TestOversizedDiffLineIsAnError(t *testing.T) {
	long := "+" + strings.Repeat("x", 17<<20)
	raw := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -1,0 +2,1 @@\n" + long + "\ndiff --git a/b.go b/b.go\n--- a/b.go\n+++ b/b.go\n@@ -1,0 +2,1 @@\n+ok\n"
	if _, err := parseDiff(raw, func(string) bool { return true }); err == nil {
		t.Fatal("parseDiff swallowed a scanner error")
	}
	log := commitRecord + strings.Repeat("a", 40) + "\x1fFirst\n" + raw
	if _, err := parseCommitLog(log); err == nil {
		t.Fatal("parseCommitLog swallowed a scanner error")
	}
}

// A change touching hundreds of files with hundreds of hunks each still
// produces a request under the state limit, because summary lists shrink
// after the text fields do.
func TestChangeRequestFitsAWideChange(t *testing.T) {
	var change Change
	for i := 0; i < 500; i++ {
		var hunks []Hunk
		for j := 0; j < 40; j++ {
			hunks = append(hunks, Hunk{OldStart: j, OldLines: 1, NewStart: j, NewLines: 1, Header: fmt.Sprintf("func Handler%d(w http.ResponseWriter, r *http.Request) error", j)})
		}
		change.Files = append(change.Files, FileChange{Path: fmt.Sprintf("pkg/service%03d/handler.go", i), Kind: FileSource, Status: FileModified, Hunks: hunks, Added: 40})
	}
	for i := 0; i < maxCommits; i++ {
		commit := Commit{SHA: fmt.Sprintf("%012d", i), Subject: fmt.Sprintf("Commit %d", i)}
		for j := 0; j < 200; j++ {
			commit.Files = append(commit.Files, fmt.Sprintf("pkg/service%03d/handler.go", j))
			commit.HunkHeaders = append(commit.HunkHeaders, fmt.Sprintf("pkg/service%03d/handler.go @@ -1,1 +1,1 @@ func Handler", j))
		}
		change.Commits = append(change.Commits, commit)
	}

	r, ok := changeRequest(change, "", "", nil, nil)
	if !ok {
		t.Fatal("no change request")
	}
	if r.oversize != "" || r.size() > maxStateChars {
		t.Fatalf("size %d oversize %q", r.size(), r.oversize)
	}
	if items, _ := r.state["items"].(map[string]any); items == nil || items[string(ItemsCommits)] == nil {
		t.Fatalf("commit items were dropped: %+v", r.state["items"])
	}
}

// A request that cannot be shrunk is reported, not sent.
func TestOversizeRequestBecomesANote(t *testing.T) {
	r := request{oversize: "state is 1 characters after shrinking; the limit is 0", pending: map[string]pending{"q": {path: "pkg/a.go", symbol: "Run"}}}
	requests, notes := keep(nil, nil, r)
	if len(requests) != 0 || len(notes) != 1 || !strings.Contains(notes[0], "pkg/a.go Run was not judged") {
		t.Fatalf("requests %d notes %v", len(requests), notes)
	}
}
