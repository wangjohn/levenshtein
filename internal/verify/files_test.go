package verify

import (
	"os"
	"path/filepath"
	"testing"
)

func TestContainedPathsAllowOnlyRelativeInternalAliases(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "app")
	if err := os.Mkdir(target, 0755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name        string
		destination string
		allowed     bool
	}{
		{"relative", "app", true},
		{"absolute", target, false},
		{"outside", t.TempDir(), false},
	} {
		if err := os.Symlink(tc.destination, filepath.Join(root, tc.name)); err != nil {
			t.Fatal(err)
		}
		_, err := contained(root, tc.name)
		if (err == nil) != tc.allowed {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}
