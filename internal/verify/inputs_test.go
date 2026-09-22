package verify

import (
	"os"
	"path/filepath"
	"testing"
)

// The root target must list its Go inputs explicitly so documentation edits do
// not invalidate cached Go analysis, on the native branch check and the Dagger
// audit alike. The fingerprint is taken over a scratch copy of the declared
// inputs so the test never edits the checkout.
func TestSelfConfigRootInputsIgnoreDocs(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}

	for run, id := range map[string]string{"branch": "native-go-lint/root", "main": "go-lint/root"} {
		t.Run(id, func(t *testing.T) {
			rootInputsIgnoreDocs(t, cfg, root, run, id)
		})
	}
}

func rootInputsIgnoreDocs(t *testing.T, cfg Config, root, run, id string) {
	t.Helper()
	plan, err := cfg.Plan(root, run)
	if err != nil {
		t.Fatal(err)
	}

	var rootLint PlannedCheck
	for _, check := range plan.Checks {
		if check.ID == id {
			rootLint = check
			break
		}
	}
	if rootLint.ID == "" {
		t.Fatalf("%s missing from %s plan", id, run)
	}
	for _, path := range rootLint.Target.Inputs {
		if path == "." {
			t.Fatalf("root target still uses broad inputs: %v", rootLint.Target.Inputs)
		}
	}

	source := t.TempDir()
	for _, path := range rootLint.Target.Inputs {
		info, err := os.Stat(filepath.Join(root, path))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(source, path)
		if info.IsDir() {
			target = filepath.Join(target, "probe.go")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte("input\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	docs := filepath.Join(source, "docs", "setup.md")
	if err := os.MkdirAll(filepath.Dir(docs), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docs, []byte("# Setup\n"), 0644); err != nil {
		t.Fatal(err)
	}

	req := Request{Source: source, Shared: root, PlannedCheck: rootLint}
	before, err := fingerprint(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(docs, []byte("# Setup\n\nedited\n"), 0644); err != nil {
		t.Fatal(err)
	}
	after, err := fingerprint(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("doc-only edit invalidated the %s fingerprint", id)
	}

	// A declared input must still change the fingerprint.
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	changed, err := fingerprint(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if changed == before {
		t.Fatalf("declared input edit did not change the %s fingerprint", id)
	}
}
