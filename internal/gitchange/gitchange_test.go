package gitchange

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/wangjohn/levenshtein/internal/testgit"
)

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

func TestChangedAdvisesFetchDepthInAShallowClone(t *testing.T) {
	origin := branchedRepo(t)
	clone := repo{dir: t.TempDir(), git: origin.git, env: origin.env}
	clone.run(t, "clone", "--quiet", "--depth", "1", "--branch", "feature", "file://"+origin.dir, clone.dir)

	_, err := clone.runner().Changed(t.Context(), clone.dir, "main")

	if err == nil || !strings.Contains(err.Error(), "fetch-depth: 0") {
		t.Fatalf("a shallow clone without the base must get fetch-depth advice: %v", err)
	}
}

// actions/checkout's default: a depth-1 feature checkout, then a depth-1
// fetch of the base. Both refs resolve, but no commit joins them.
func TestChangedAdvisesFetchDepthWhenAShallowBaseSharesNoCommit(t *testing.T) {
	origin := branchedRepo(t)
	clone := repo{dir: t.TempDir(), git: origin.git, env: origin.env}
	clone.run(t, "clone", "--quiet", "--depth", "1", "--branch", "feature", "file://"+origin.dir, clone.dir)
	clone.run(t, "fetch", "--quiet", "--depth", "1", "origin", "main:refs/remotes/origin/main")

	_, err := clone.runner().Changed(t.Context(), clone.dir, "main")

	if err == nil || !strings.Contains(err.Error(), "shallow") || !strings.Contains(err.Error(), "fetch-depth: 0") {
		t.Fatalf("a shallow base without a merge base must get fetch-depth advice: %v", err)
	}
}

func TestResolveBaseNamesUnrelatedHistories(t *testing.T) {
	r := branchedRepo(t)
	r.run(t, "switch", "--quiet", "--orphan", "unrelated")
	r.write(t, "alone.go", "package x\n")
	r.run(t, "add", "alone.go")
	r.run(t, "commit", "--quiet", "-m", "unrelated")

	_, _, err := r.runner().ResolveBase(t.Context(), "main")

	if err == nil || err.Error() != "no common ancestor between main and HEAD" {
		t.Fatalf("unrelated histories must be named: %v", err)
	}
}

// historyClone clones a remote whose main has two commits, older and newer,
// onto its feature branch, which starts from newer. The clone has no local
// main until a test makes one.
func historyClone(t *testing.T) (repo, string, string) {
	t.Helper()
	origin := newRepo(t)
	origin.write(t, "a.go", "package x\n")
	origin.run(t, "add", ".")
	origin.run(t, "commit", "--quiet", "-m", "older")
	older := strings.TrimSpace(testgit.Run(t, origin.git, origin.dir, "rev-parse", "HEAD"))
	origin.write(t, "b.go", "package x\n")
	origin.run(t, "add", ".")
	origin.run(t, "commit", "--quiet", "-m", "newer")
	newer := strings.TrimSpace(testgit.Run(t, origin.git, origin.dir, "rev-parse", "HEAD"))
	origin.run(t, "switch", "--quiet", "-c", "feature")
	origin.write(t, "c.go", "package x\n")
	origin.run(t, "add", ".")
	origin.run(t, "commit", "--quiet", "-m", "feature")

	clone := repo{dir: t.TempDir(), git: origin.git, env: origin.env}
	clone.run(t, "clone", "--quiet", "--branch", "feature", "file://"+origin.dir, clone.dir)
	return clone, older, newer
}

func TestResolveBasePrefersTheCurrentOfTheTwoBaseRefs(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setup   func(t *testing.T, r repo, older string)
		wantRef string
	}{
		{
			name: "stale local branch",
			setup: func(t *testing.T, r repo, older string) {
				r.run(t, "branch", "main", older)
			},
			wantRef: "origin/main",
		},
		{
			name: "local branch ahead of origin",
			setup: func(t *testing.T, r repo, _ string) {
				r.run(t, "branch", "main", "origin/main")
				r.run(t, "switch", "--quiet", "main")
				r.run(t, "commit", "--quiet", "--allow-empty", "-m", "unpushed")
				r.run(t, "switch", "--quiet", "feature")
			},
			wantRef: "main",
		},
		{
			name: "local branch equal to origin",
			setup: func(t *testing.T, r repo, _ string) {
				r.run(t, "branch", "main", "origin/main")
			},
			wantRef: "main",
		},
		{
			name: "diverged local branch",
			setup: func(t *testing.T, r repo, older string) {
				r.run(t, "branch", "main", older)
				r.run(t, "switch", "--quiet", "main")
				r.run(t, "commit", "--quiet", "--allow-empty", "-m", "local only")
				r.run(t, "switch", "--quiet", "feature")
			},
			wantRef: "origin/main",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, older, newer := historyClone(t)
			tc.setup(t, r, older)

			ref, mergeBase, err := r.runner().ResolveBase(t.Context(), "main")
			if err != nil {
				t.Fatal(err)
			}

			if ref != tc.wantRef {
				t.Errorf("ref = %q, want %q", ref, tc.wantRef)
			}
			if mergeBase != newer {
				t.Errorf("merge base = %s, want origin's newer commit %s (older is %s)", mergeBase, newer, older)
			}
		})
	}
}

func TestSourcePrefixRejectsASourceOutsideTheWorktree(t *testing.T) {
	top := t.TempDir()

	_, err := SourcePrefix(top, t.TempDir())

	if err == nil || !strings.Contains(err.Error(), "outside the git worktree") {
		t.Fatalf("outside source: %v", err)
	}
}

func TestChangedReportsTheLinesEachFileChanged(t *testing.T) {
	r := newRepo(t)
	r.write(t, "app/edit.go", "one\ntwo\nthree\nfour\nfive\n")
	r.write(t, "app/trim.go", "keep\ndrop\n")
	r.write(t, "app/mode.go", "same\n")
	r.write(t, "app/q\"uote.go", "one\n")
	r.run(t, "add", ".")
	r.run(t, "commit", "--quiet", "-m", "base")
	r.run(t, "switch", "--quiet", "-c", "feature")
	r.write(t, "app/edit.go", "one\nTWO\nthree\nfour\nfive\nsix\nseven\n")
	r.write(t, "app/trim.go", "keep\n")
	r.write(t, "app/q\"uote.go", "one\ntwo\n")
	if err := os.Chmod(filepath.Join(r.dir, "app", "mode.go"), 0o755); err != nil {
		t.Fatal(err)
	}
	r.write(t, "app/new.go", "fresh\n")

	got, err := r.runner().Changed(t.Context(), r.dir, "main")
	if err != nil {
		t.Fatal(err)
	}

	if want := []Range{{Start: 2, End: 2}, {Start: 6, End: 7}}; !slices.Equal(got.Lines["app/edit.go"], want) {
		t.Errorf("edit.go lines = %v, want %v", got.Lines["app/edit.go"], want)
	}
	if ranges, ok := got.Lines["app/trim.go"]; !ok || len(ranges) != 0 {
		t.Errorf("a deletion-only change must have an entry with no lines: %v, %v", ranges, ok)
	}
	if _, ok := got.Lines["app/new.go"]; ok {
		t.Errorf("an untracked file has no entry, because all of it is new: %v", got.Lines)
	}
	// A mode-only change prints no hunk header; it changed no lines, rather
	// than counting every line as new.
	if ranges, ok := got.Lines["app/mode.go"]; !ok || len(ranges) != 0 {
		t.Errorf("a mode-only change must have an entry with no lines: %v, %v", ranges, ok)
	}
	// Git quotes this name in the diff header even with core.quotePath off.
	if want := []Range{{Start: 2, End: 2}}; !slices.Equal(got.Lines["app/q\"uote.go"], want) {
		t.Errorf("quoted name lines = %v, want %v (all: %v)", got.Lines["app/q\"uote.go"], want, got.Lines)
	}
}

func TestChangedLinesReadsOnlyRealFileHeaders(t *testing.T) {
	diff := strings.Join([]string{
		"diff --git a/with space.go b/with space.go",
		"--- a/with space.go\t",
		"+++ b/with space.go\t",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		`diff --git "a/q\"uote.go" "b/q\"uote.go"`,
		`--- "a/q\"uote.go"`,
		`+++ "b/q\"uote.go"`,
		"@@ -1,0 +2 @@",
		"+x",
		"diff --git a/tricky.go b/tricky.go",
		"--- a/tricky.go",
		"+++ b/tricky.go",
		"@@ -3,0 +4,2 @@",
		"+++ b/not-a-header.go",
		"+second",
		"@@ -9 +10,0 @@",
		"-gone",
	}, "\n")

	got := changedLines(diff)

	if want := []Range{{Start: 1, End: 1}}; !slices.Equal(got["with space.go"], want) {
		t.Errorf("tab-terminated name: %v", got)
	}
	if want := []Range{{Start: 4, End: 5}}; !slices.Equal(got["tricky.go"], want) {
		t.Errorf("tricky.go = %v, want %v (a pure deletion adds no range)", got["tricky.go"], want)
	}
	if want := []Range{{Start: 2, End: 2}}; !slices.Equal(got[`q"uote.go`], want) {
		t.Errorf("quoted name: %v", got)
	}
	if _, ok := got["not-a-header.go"]; ok {
		t.Errorf("an added line beginning with ++ was read as a header: %v", got)
	}
}
