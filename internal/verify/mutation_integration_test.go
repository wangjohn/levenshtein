//go:build integration

package verify

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A go-mutation check bounds its own run with a timeout, but the Dagger
// session belongs to the whole run. Two checks on one runner must both reach
// the engine; before the fix the first one closed the session on return.
func TestMutationKeepsTheSharedDaggerSession(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}
	shared, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	source, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), git, args...)
		cmd.Dir = source
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	fixture := filepath.Join(shared, "runner", "testdata", "mutation", "strong")
	for _, name := range []string{"go.mod", "add.go", "add_test.go"} {
		data, err := os.ReadFile(filepath.Join(fixture, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(source, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "--quiet", "--initial-branch=main")
	run("add", ".")
	run("commit", "--quiet", "-m", "base")
	run("switch", "--quiet", "-c", "feature")
	add := filepath.Join(source, "add.go")
	data, err := os.ReadFile(add)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(add, append(data, []byte("\n// Changed on the branch.\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GITHUB_BASE_REF", "")
	runner := &Dagger{}
	defer func() { _ = runner.Close() }()

	for _, id := range []string{"first", "second"} {
		result := runner.Execute(t.Context(), Request{Source: source, Shared: shared, PlannedCheck: PlannedCheck{
			ID:          id,
			Check:       Check{Kind: CheckGoMutation},
			Target:      Target{Dir: ".", Workspace: ".", Inputs: []string{"."}},
			Environment: Environment{Executor: ExecutorDagger},
		}})

		if result.Status != StatusPassed || !strings.Contains(result.Stdout, "1 files mutated") {
			t.Fatalf("%s check: %+v", id, result)
		}
	}
}
