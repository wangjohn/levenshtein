package gitchange

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type repo struct {
	dir string
	git string
	env []string
}

func newRepo(t *testing.T) repo {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	// Isolate git configuration without changing HOME; Apple's git shim reads its license state from there.
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	r := repo{dir: t.TempDir(), git: git, env: env}
	r.run(t, "init", "--quiet", "--initial-branch=main")
	return r
}

func (r repo) run(t *testing.T, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), r.git, args...)
	cmd.Dir = r.dir
	cmd.Env = r.env
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (r repo) write(t *testing.T, path, content string) {
	t.Helper()
	full := filepath.Join(r.dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (r repo) runner() Runner {
	return Runner{Git: r.git, Dir: r.dir, Env: r.env}
}

// branchedRepo commits kept.go, edited.go, and gone.go on main, then on a
// feature branch edits one, deletes one, adds one, and leaves one untracked.
func branchedRepo(t *testing.T) repo {
	t.Helper()
	r := newRepo(t)
	for _, name := range []string{"app/kept.go", "app/edited.go", "app/gone.go", "other/root.go"} {
		r.write(t, name, "package x\n")
	}
	r.write(t, ".gitignore", "ignored.go\n")
	r.run(t, "add", ".")
	r.run(t, "commit", "--quiet", "-m", "base")

	r.run(t, "switch", "--quiet", "-c", "feature")
	r.write(t, "app/edited.go", "package x\n\nvar edited = 1\n")
	r.run(t, "rm", "--quiet", "app/gone.go")
	r.write(t, "app/added.go", "package x\n")
	r.run(t, "add", ".")
	r.run(t, "commit", "--quiet", "-m", "feature")
	r.write(t, "app/untracked.go", "package x\n")
	r.write(t, "app/ignored.go", "package x\n")
	return r
}

func TestChangedListsEditsAdditionsAndUntrackedFiles(t *testing.T) {
	r := branchedRepo(t)

	got, err := r.runner().Changed(t.Context(), r.dir, "main")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"app/added.go", "app/edited.go", "app/untracked.go"}
	if !slices.Equal(got.Paths, want) {
		t.Fatalf("Paths = %v, want %v (deleted, ignored, and unchanged files left out)", got.Paths, want)
	}
	if got.BaseRef != "main" || len(got.Base) != 40 {
		t.Fatalf("base = %q at %q, want main at a full commit", got.BaseRef, got.Base)
	}
}

func TestChangedIsRelativeToASourceBelowTheToplevel(t *testing.T) {
	r := branchedRepo(t)

	source := filepath.Join(r.dir, "app")
	got, err := Runner{Git: r.git, Dir: source, Env: r.env}.Changed(t.Context(), source, "main")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"added.go", "edited.go", "untracked.go"}
	if !slices.Equal(got.Paths, want) {
		t.Fatalf("Paths = %v, want %v", got.Paths, want)
	}
}

func TestChangedFallsBackToTheOriginRef(t *testing.T) {
	origin := branchedRepo(t)
	clone := repo{dir: t.TempDir(), git: origin.git, env: origin.env}
	clone.run(t, "clone", "--quiet", "--branch", "feature", "file://"+origin.dir, clone.dir)

	got, err := clone.runner().Changed(t.Context(), clone.dir, "main")
	if err != nil {
		t.Fatal(err)
	}

	if got.BaseRef != "origin/main" {
		t.Fatalf("BaseRef = %q, want origin/main when only the tracking ref exists", got.BaseRef)
	}
	if !slices.Equal(got.Paths, []string{"app/added.go", "app/edited.go"}) {
		t.Fatalf("Paths = %v", got.Paths)
	}
}

func TestChangedNamesAMissingBase(t *testing.T) {
	r := branchedRepo(t)

	_, err := r.runner().Changed(t.Context(), r.dir, "release")

	if err == nil || !strings.Contains(err.Error(), `"release"`) {
		t.Fatalf("missing base must be named: %v", err)
	}
}

func TestSourcePrefixRejectsASourceOutsideTheWorktree(t *testing.T) {
	top := t.TempDir()

	_, err := SourcePrefix(top, t.TempDir())

	if err == nil || !strings.Contains(err.Error(), "outside the git worktree") {
		t.Fatalf("outside source: %v", err)
	}
}
