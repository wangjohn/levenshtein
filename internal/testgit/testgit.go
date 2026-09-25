// Package testgit runs git for tests in throwaway repositories without ever
// reaching the repository the tests were started from.
//
// Git obeys GIT_DIR, GIT_WORK_TREE, GIT_INDEX_FILE, and their relatives over
// the working directory. Git exports some of them to hooks, so `go test` from
// a pre-push hook would otherwise send a fixture's init, add, and commit to
// the caller's own repository. Every test that runs git goes through this
// package; a guard test here fails on a test file that runs git directly.
package testgit

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// isolation replaces the caller's git configuration and identity. HOME is
// kept, because Apple's git shim reads its license state from there.
var isolation = []string{
	"GIT_CONFIG_GLOBAL=" + os.DevNull,
	"GIT_CONFIG_NOSYSTEM=1",
	"GIT_AUTHOR_NAME=t",
	"GIT_AUTHOR_EMAIL=t@example.com",
	"GIT_COMMITTER_NAME=t",
	"GIT_COMMITTER_EMAIL=t@example.com",
}

// inherited reports whether an environment entry belongs to git. The prefix
// covers the repository variables and GIT_CONFIG_COUNT, GIT_CONFIG_KEY_n, and
// GIT_CONFIG_PARAMETERS alike.
func inherited(entry string) bool {
	return strings.HasPrefix(entry, "GIT_")
}

// Env is the process environment without any GIT_ variable, plus a fixed
// configuration and identity.
func Env() []string {
	var env []string
	for _, entry := range os.Environ() {
		if !inherited(entry) {
			env = append(env, entry)
		}
	}
	return append(env, isolation...)
}

// Isolate gives the process environment the same treatment for the rest of
// the test, for code under test that runs git with the process environment.
// Like t.Setenv, it cannot be used in a parallel test.
func Isolate(tb testing.TB) {
	tb.Helper()
	for _, entry := range os.Environ() {
		if !inherited(entry) {
			continue
		}
		name, _, _ := strings.Cut(entry, "=")
		tb.Setenv(name, "") // Registers the restore; the unset below is what the test sees.
		if err := os.Unsetenv(name); err != nil {
			tb.Fatal(err)
		}
	}
	for _, entry := range isolation {
		name, value, _ := strings.Cut(entry, "=")
		tb.Setenv(name, value)
	}
}

// Path finds git, or skips the test when it is not installed.
func Path(tb testing.TB) string {
	tb.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		tb.Skip("git is not installed")
	}
	return git
}

// Command prepares git to run in dir with Env.
func Command(ctx context.Context, git, dir string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, git, args...)
	cmd.Dir = dir
	cmd.Env = Env()
	return cmd
}

// Run runs git in dir with Env and returns its combined output. It fails the
// test when git fails.
func Run(tb testing.TB, git, dir string, args ...string) string {
	tb.Helper()
	out, err := Command(tb.Context(), git, dir, args...).CombinedOutput()
	if err != nil {
		tb.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
