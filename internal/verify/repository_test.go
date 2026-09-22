package verify

import (
	"os"
	"path/filepath"
	"testing"
)

// The runner target compiles against the generated Dagger SDK, which is
// gitignored. It declares filesystem discovery so an edit to those files
// changes its key; under git discovery the same edit would be invisible.
func TestRunnerTargetFingerprintsItsGeneratedSDK(t *testing.T) {
	requireGit(t)
	cfg, err := Load("../..")
	if err != nil {
		t.Fatal(err)
	}
	shared, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}

	target := cfg.Targets["runner"]
	if target.Discovery != DiscoveryFilesystem {
		t.Fatalf("runner target discovery is %q, want %q", target.Discovery, DiscoveryFilesystem)
	}

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "init")
	ignore, err := os.ReadFile(filepath.Join(shared, "runner", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "runner", ".gitignore"), string(ignore))
	writeFile(t, filepath.Join(root, "runner", "main.go"), sourceOne)
	generated := filepath.Join(root, "runner", "dagger.gen.go")
	writeFile(t, generated, sourceOne)
	runGit(t, root, "add", ".")

	key := func(discovery DiscoveryKind) string {
		t.Helper()
		planned := target
		planned.Discovery = discovery
		req := Request{
			Source: root,
			Shared: shared,
			PlannedCheck: PlannedCheck{
				ID:          "native-go-lint/runner",
				Check:       Check{Kind: CheckGoLint, Target: "runner", Environment: "host-go"},
				Target:      planned,
				Environment: cfg.Environments["host-go"],
			},
		}
		got, err := fingerprint(req)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}

	beforeFilesystem, beforeGit := key(DiscoveryFilesystem), key(DiscoveryGit)
	writeFile(t, generated, "package one\n\nconst regenerated = true\n")
	if key(DiscoveryFilesystem) == beforeFilesystem {
		t.Error("editing the generated SDK kept the runner target's key")
	}
	if key(DiscoveryGit) != beforeGit {
		t.Error("git discovery saw an ignored file; this test no longer proves why the runner target opts out")
	}
}
