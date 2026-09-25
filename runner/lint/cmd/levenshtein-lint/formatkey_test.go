package main

import (
	"os"
	"path/filepath"
	"testing"
)

// The digest keys Staticcheck's cache on the files the build leaves out: none
// leaves the key alone, and editing one changes it.
func TestIgnoredDigestFollowsExcludedFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, source string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	digest := func() string {
		t.Helper()
		got, err := ignoredDigest([]string{"./..."})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	t.Chdir(dir)
	write("go.mod", "module example.com/digest\n\ngo 1.27.1\n")
	write("digest.go", "package digest\n")

	if got := digest(); got != "" {
		t.Errorf("digest with no excluded files = %q, want none", got)
	}

	write("integration.go", "//go:build integration\n\npackage digest\nvar  x = 1\n")
	unformatted := digest()
	write("integration.go", "//go:build integration\n\npackage digest\n\nvar x = 1\n")
	formatted := digest()

	if unformatted == "" || formatted == "" || unformatted == formatted {
		t.Errorf("digests before and after formatting the excluded file = %q and %q, want two different digests", unformatted, formatted)
	}
}
