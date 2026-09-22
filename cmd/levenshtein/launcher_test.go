package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLauncherIgnoresCallerToolchain(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	caller := t.TempDir()
	if err := os.WriteFile(filepath.Join(caller, "go.work"), []byte("go 1.99.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), filepath.Join(root, "verify"), "branch", "--dry-run")
	cmd.Dir = caller
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=auto", "GOWORK="+filepath.Join(caller, "go.work"), "GOPROXY=off", "XDG_CACHE_HOME="+t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("consumer toolchain affected launcher: %v\n%s", err, out)
	}
}
