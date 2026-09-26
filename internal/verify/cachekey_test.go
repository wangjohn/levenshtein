package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/wangjohn/levenshtein/internal/testgit"
)

// Every worktree of one clone shares a key, and so its warm untrusted caches;
// separate clones, and directories outside any work tree, get keys of their
// own; and a symlinked path names the same key as the directory it resolves to.
func TestRepositoryKeyIsTheClone(t *testing.T) {
	git := testgit.Path(t)
	testgit.Isolate(t)
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	worktree := filepath.Join(base, "worktree")
	clone := filepath.Join(base, "clone")
	plain := filepath.Join(base, "plain")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	testgit.Run(t, git, repo, "init", "--quiet")
	testgit.Run(t, git, repo, "commit", "--quiet", "--allow-empty", "-m", "initial")
	testgit.Run(t, git, repo, "worktree", "add", "--quiet", "--detach", worktree)
	testgit.Run(t, git, base, "clone", "--quiet", repo, clone)
	linkedRepo := filepath.Join(base, "linked-repo")
	linkedPlain := filepath.Join(base, "linked-plain")
	if err := os.Symlink(repo, linkedRepo); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(plain, linkedPlain); err != nil {
		t.Fatal(err)
	}

	key := func(dir string) string {
		t.Helper()
		return repositoryKey(t.Context(), dir)
	}
	resolvedPlain, err := filepath.EvalSymlinks(plain)
	if err != nil {
		t.Fatal(err)
	}
	plainSum := sha256.Sum256([]byte(resolvedPlain))

	if key(repo) != key(worktree) {
		t.Errorf("a worktree must share its clone's key: %s and %s", key(repo), key(worktree))
	}
	if key(repo) == key(clone) {
		t.Errorf("separate clones must not share a key: %s", key(repo))
	}
	if key(plain) != hex.EncodeToString(plainSum[:]) || key(plain) == key(repo) {
		t.Errorf("a directory outside any work tree must be keyed by its resolved path: %s", key(plain))
	}
	if key(linkedRepo) != key(repo) || key(linkedPlain) != key(plain) {
		t.Errorf("a symlinked path must name the key of the directory it resolves to: %s, %s", key(linkedRepo), key(linkedPlain))
	}
	if len(key(repo)) != 64 {
		t.Errorf("the key must be a hex SHA-256, as the runner requires: %q", key(repo))
	}
}

// The environment the CLI runs in cannot point the key at another clone, so
// neither can a hook or a wrapper script that sets GIT_DIR.
func TestRepositoryKeyIgnoresGitEnvironment(t *testing.T) {
	git := testgit.Path(t)
	testgit.Isolate(t)
	base := t.TempDir()
	repo := filepath.Join(base, "repo")
	other := filepath.Join(base, "other")
	for _, dir := range []string{repo, other} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		testgit.Run(t, git, dir, "init", "--quiet")
	}
	want := repositoryKey(t.Context(), repo)

	t.Setenv("GIT_DIR", filepath.Join(other, ".git"))
	t.Setenv("GIT_COMMON_DIR", filepath.Join(other, ".git"))

	if got := repositoryKey(t.Context(), repo); got != want {
		t.Errorf("GIT_DIR changed the key from %s to %s", want, got)
	}
}
