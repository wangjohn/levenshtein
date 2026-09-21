package verify

import (
	"os"
	"path/filepath"
	"testing"
)

// The root target must list its Go inputs explicitly so documentation edits do
// not invalidate cached Go analysis. The fingerprint is taken over a scratch
// copy of the declared inputs so the test never edits the checkout.
func TestSelfConfigRootInputsIgnoreDocs(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := cfg.Plan(root, "branch")
	if err != nil {
		t.Fatal(err)
	}

	var rootLint PlannedCheck
	for _, check := range plan.Checks {
		if check.ID == "go-lint/root" {
			rootLint = check
			break
		}
	}
	if rootLint.ID == "" {
		t.Fatal("go-lint/root missing from branch plan")
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
	before, err := fingerprint(req)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(docs, []byte("# Setup\n\nedited\n"), 0644); err != nil {
		t.Fatal(err)
	}
	after, err := fingerprint(req)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("doc-only edit invalidated root go-lint fingerprint")
	}

	// A declared input must still change the fingerprint.
	if err := os.WriteFile(filepath.Join(source, "go.mod"), []byte("changed\n"), 0644); err != nil {
		t.Fatal(err)
	}
	changed, err := fingerprint(req)
	if err != nil {
		t.Fatal(err)
	}
	if changed == before {
		t.Fatal("declared input edit did not change root go-lint fingerprint")
	}
}
