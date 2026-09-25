package verify

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A check that reads the target's files sees what the Dagger path imports:
// declared inputs less excludes, private files, and symlinks.
func TestVisibleFilesMatchTheDaggerImport(t *testing.T) {
	source := t.TempDir()
	for _, path := range []string{"app/main.sh", "app/.env", "app/.env.local", "app/.env.example", "app/build/out.sh", "docs/readme.md", "other/x.sh", "top.sh"} {
		writeTestFile(t, filepath.Join(source, filepath.FromSlash(path)), "x\n")
	}
	if err := os.Symlink(filepath.Join(source, "other", "x.sh"), filepath.Join(source, "app", "link.sh")); err != nil {
		t.Fatal(err)
	}

	files, err := visibleFiles(source, []string{"app", "docs", "top.sh", "missing", "app"}, []string{filepath.Join("app", "build")}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"app/.env.example", "app/main.sh", "docs/readme.md", "top.sh"}; !slices.Equal(files, want) {
		t.Fatalf("visible files %v, want %v", files, want)
	}
}

func TestStageFilesCopiesOnlyTheNamedFiles(t *testing.T) {
	source := t.TempDir()
	writeTestFile(t, filepath.Join(source, "a", "b.txt"), "b\n")
	writeTestFile(t, filepath.Join(source, "c.txt"), "c\n")

	staged, cleanup, err := stageFiles(source, []string{"a/b.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(staged, "a", "b.txt")); err != nil || string(got) != "b\n" {
		t.Fatalf("staged %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(staged, "c.txt")); !os.IsNotExist(err) {
		t.Fatalf("an unnamed file was staged: %v", err)
	}

	cleanup()
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Fatalf("cleanup must remove the staged directory: %v", err)
	}
}
