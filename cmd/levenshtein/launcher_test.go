package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	cmd := exec.Command(filepath.Join(root, "verify"), "branch", "--dry-run")
	cmd.Dir = caller
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=auto", "GOWORK="+filepath.Join(caller, "go.work"), "GOPROXY=off", "XDG_CACHE_HOME="+t.TempDir())
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("consumer toolchain affected launcher: %v\n%s", err, out)
	}
}

// The launcher asks Go for the pinned toolchain, so the only Go requirement it
// states is that some go exists. The message names the pinned version and the
// document that explains how to obtain it.
func TestLauncherNeedsOnlyAGoOnPath(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := os.ReadFile(filepath.Join(root, ".go-version"))
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(filepath.Join(root, "verify"), "branch", "--dry-run")
	cmd.Env = []string{"PATH=/usr/bin:/bin", "HOME=" + t.TempDir()}
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("launcher built without a Go on PATH:\n%s", out)
	}
	if !strings.Contains(string(out), strings.TrimSpace(string(pinned))) || !strings.Contains(string(out), "docs/setup.md") {
		t.Fatalf("message names neither the pinned version nor setup:\n%s", out)
	}
}
