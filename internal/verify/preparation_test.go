package verify

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestReviewConflictingPreparation(t *testing.T) {
	req := nativeRequest(t)
	req.Environment.Identity = "conflicting-preparation-fixture"
	prepA := &Preparation{Command: []string{"/bin/sh", "-c", "printf A > ready"}, Inputs: []string{"lock"}, Outputs: []string{"ready"}}
	prepB := &Preparation{Command: []string{"/bin/sh", "-c", "printf B > ready"}, Inputs: []string{"lock"}, Outputs: []string{"ready"}}
	req.Check.Command = []string{"/bin/sh", "-c", "cat ready"}
	n := &Native{}
	for i, prep := range []*Preparation{prepA, prepB, prepA} {
		req.Preparation = prep
		got := n.Execute(context.Background(), req)
		t.Logf("step %d: %s %q", i, got.Status, got.Stdout)
		if i == 2 && got.Stdout != "A" {
			t.Errorf("reused incompatible preparation: got %q, wanted A", got.Stdout)
		}
	}
}

func TestReviewToolEnvironment(t *testing.T) {
	req := nativeRequest(t)
	root := req.Source
	for _, version := range []string{"expected", "wrong"} {
		dir := filepath.Join(root, version)
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tool"), []byte("#!/bin/sh\nprintf "+version), 0755); err != nil {
			t.Fatal(err)
		}
	}

	req.Environment.Env = map[string]string{"PATH": filepath.Join(root, "expected")}
	req.Environment.Tools = []Tool{{Command: []string{"tool"}, Version: "expected"}}
	req.Check.Env = map[string]string{"PATH": filepath.Join(root, "wrong")}
	req.Check.Command = []string{"tool"}
	got := (&Native{}).Execute(context.Background(), req)
	if got.Status == StatusPassed && got.Stdout == "wrong" {
		t.Errorf("version validation passed but executed wrong tool: %+v", got)
	}
}
