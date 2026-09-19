package verify

import (
	"os"
	"path/filepath"
	"testing"
)

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

	req := Request{Source: root, Shared: root, PlannedCheck: rootLint}
	before, err := fingerprint(req)
	if err != nil {
		t.Fatal(err)
	}

	docs := filepath.Join(root, "docs", "setup.md")
	original, err := os.ReadFile(docs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(docs, original, 0644); err != nil {
			t.Errorf("restore docs: %v", err)
		}
	})
	if err := os.WriteFile(docs, append(original, []byte("\n// phase-6 fingerprint probe\n")...), 0644); err != nil {
		t.Fatal(err)
	}

	after, err := fingerprint(req)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("doc-only edit invalidated root go-lint fingerprint")
	}
}
