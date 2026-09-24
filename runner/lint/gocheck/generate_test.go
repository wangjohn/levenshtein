package gocheck

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// copyTree copies a fixture into a scratch directory the check may change, as
// both executors do before running it.
func copyTree(t *testing.T, from string) string {
	t.Helper()
	to := t.TempDir()
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(to, rel), data, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return to
}

func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGeneratePassesFreshOutputAndIgnoresIgnoredFiles(t *testing.T) {
	root := copyTree(t, filepath.Join(testdata, "generate-fresh"))
	report, err := Generate(t.Context(), root, ".", testEnv())
	if err != nil || len(report.Findings) != 0 {
		t.Fatalf("generate-fresh must pass: %+v %v", report, err)
	}
	if _, err := os.Stat(filepath.Join(root, "build", "palette.txt")); err != nil {
		t.Fatalf("the generator did not run, so the pass proves nothing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Fatalf("the scratch repository must live outside the tree generators see: %v", err)
	}
}

func TestGenerateReportsAStaleFileWithItsDiff(t *testing.T) {
	root := copyTree(t, filepath.Join(testdata, "generate-stale"))
	report, err := Generate(t.Context(), root, ".", testEnv())
	if err != nil {
		t.Fatalf("generate-stale must fail for its stale file, not error: %v", err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("want one finding: %+v", report)
	}
	finding := report.Findings[0]
	if finding.Code != CodeGenerate || finding.Location.File != "names_gen.go" || finding.Location.Line != 9 || !strings.Contains(finding.Message, "+\t\"blue\",") {
		t.Fatalf("finding lost its location or diff: %+v", finding)
	}
}

func TestGenerateReportsAddedAndDeletedFilesInANestedModule(t *testing.T) {
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"svc/go.mod":        "module example.com/svc\n\ngo 1.24\n",
		"svc/svc.go":        "package svc\n\n//go:generate sh -c \"echo new > added.txt && rm obsolete.txt\"\n",
		"svc/obsolete.txt":  "old\n",
		"svc/untouched.txt": "kept\n",
	})
	report, err := Generate(t.Context(), root, "svc", testEnv())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, finding := range report.Findings {
		got[finding.Location.File] = finding.Message
	}
	if len(got) != 2 || !strings.Contains(got["svc/added.txt"], "creates this file") || !strings.Contains(got["svc/obsolete.txt"], "deletes this file") {
		t.Fatalf("want one added and one deleted file, by repository path: %+v", report.Findings)
	}
}

func TestGenerateRefusesWhatItCannotJudge(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"no directives", map[string]string{"go.mod": "module example.com/none\n\ngo 1.24\n", "none.go": "package none\n\n// go:generate is not a directive\n"}, []string{"no //go:generate directives; refusing an empty pass"}},
		{"missing tool", map[string]string{"go.mod": "module example.com/proto\n\ngo 1.24\n", "proto.go": "package proto\n\n//go:generate levenshtein-missing-protoc --go_out=.\n"}, []string{"exited 1", "executable file not found", "belongs in a command check"}},
		{"no module", map[string]string{"loose.go": "package loose\n"}, []string{"needs a readable go.mod"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeFiles(t, root, tc.files)
			_, err := Generate(t.Context(), root, ".", testEnv())
			for _, want := range tc.want {
				if err == nil || !strings.Contains(err.Error(), want) {
					t.Fatalf("want an error containing %q, got %v", want, err)
				}
			}
		})
	}
}

// A modified file is located at its first changed line, past the context
// lines that open the hunk.
func TestFirstChangedLineSkipsTheHunkContext(t *testing.T) {
	for _, tc := range []struct {
		diff string
		want int
	}{
		{"diff --git a/x b/x\n@@ -6,4 +6,5 @@ var\n a\n b\n c\n+d\n e\n", 9},
		{"@@ -1,3 +1,3 @@\n-a\n+b\n c\n", 1},
		{"@@ -10,7 +10,6 @@\n a\n b\n c\n-d\n e\n", 13},
		{"@@ -0,0 +1 @@\n+new\n", 1},
		{"old mode 100644\nnew mode 100755\n", 1},
	} {
		if got := firstChangedLine(tc.diff); got != tc.want {
			t.Errorf("firstChangedLine(%q) = %d, want %d", tc.diff, got, tc.want)
		}
	}
}

func TestCapDiffKeepsTheStart(t *testing.T) {
	long := strings.Repeat("+line\n", maxDiffLines+50)
	capped := capDiff(long)
	if strings.Count(capped, "+line") != maxDiffLines || !strings.HasSuffix(capped, "[diff truncated: 50 more lines]") {
		t.Fatalf("capped diff: %q", capped[len(capped)-80:])
	}
	if short := "@@ -1 +1 @@\n-a\n+b\n"; capDiff(short) != strings.TrimRight(short, "\n") {
		t.Fatalf("a short diff must be kept whole: %q", capDiff(short))
	}
}
