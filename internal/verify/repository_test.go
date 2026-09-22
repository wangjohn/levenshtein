package verify

import (
	"os"
	"path/filepath"
	"testing"
)

// The runner module compiles against the generated Dagger SDK, which is
// gitignored, so the runner target's git discovery never lists it. Levenshtein
// verifies itself with one directory as both the shared checkout and the
// source, and every shared Go check hashes all of the shared runner/ from the
// filesystem, so an SDK edit still changes the key. This pins that layout.
func TestSelfVerificationFingerprintsTheGeneratedSDK(t *testing.T) {
	requireGit(t)
	cfg, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	repository, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	// Take the checks as planned, so the target carries the discovery default
	// the planner applies rather than the configuration's empty field.
	planned := map[string]PlannedCheck{}
	for _, run := range []string{"branch", "branch-dagger"} {
		plan, err := cfg.Plan(repository, run)
		if err != nil {
			t.Fatal(err)
		}
		for _, check := range plan.Checks {
			planned[check.ID] = check
		}
	}

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init")
	ignore, err := os.ReadFile(filepath.Join(repository, "runner", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "runner", ".gitignore"), string(ignore))
	writeFile(t, filepath.Join(root, "runner", "main.go"), sourceOne)
	generated := filepath.Join(root, "runner", "dagger.gen.go")
	writeFile(t, generated, sourceOne)
	runGit(t, root, "add", ".")

	for _, id := range []string{"native-go-lint/runner", "go-lint/runner"} {
		check, ok := planned[id]
		if !ok {
			t.Fatalf("%s is not planned", id)
		}
		if check.Target.Discovery != DiscoveryGit {
			t.Fatalf("%s: discovery is %q; this test pins the git-discovery layout", id, check.Target.Discovery)
		}
		req := Request{Source: root, Shared: root, PlannedCheck: check}
		key := func() string {
			t.Helper()
			// A new CLI process takes a new snapshot of the shared checkout.
			implementations.Clear()
			got, err := fingerprint(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			return got
		}

		before := key()
		writeFile(t, generated, "package one\n\nconst regenerated = true\n")
		if key() == before {
			t.Errorf("%s: editing the generated SDK kept the key", id)
		}
		writeFile(t, generated, sourceOne)
	}
}
