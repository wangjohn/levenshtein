//go:build darwin || linux

package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicArtifactKeepsRootAndPermissions(t *testing.T) {
	parent := t.TempDir()
	source := filepath.Join(parent, "source")
	if err := os.Mkdir(source, 0700); err != nil {
		t.Fatal(err)
	}
	root, err := os.OpenRoot(source)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := atomicWriteRoot(root, "reports/test.txt", []byte("old"), 0755); err != nil {
		t.Fatal(err)
	}
	// A renamed checkout must not redirect a pending restore into a new symlink.
	moved := filepath.Join(parent, "moved")
	if err := os.Rename(source, moved); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, source); err != nil {
		t.Fatal(err)
	}
	if err := atomicWriteRoot(root, "reports/test.txt", []byte("restored"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(moved, "reports/test.txt"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("artifact permissions: %v, %v", info, err)
	}
	data, err := os.ReadFile(filepath.Join(moved, "reports/test.txt"))
	if err != nil || string(data) != "restored" {
		t.Fatalf("restored artifact: %q, %v", data, err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("restore escaped the original root: %v, %v", entries, err)
	}
}
