//go:build darwin || linux

package verify

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

const executableFixtureOutput = "native publication fixture\nPASS\n"

// The current test executable supplies a native binary without requiring a
// compiler or downloaded fixture. Only the selected child test writes output.
func TestExecutableFixture(t *testing.T) {
	if os.Getenv("LEVENSHTEIN_EXECUTABLE_FIXTURE") != "1" {
		return
	}
	if _, err := fmt.Fprintln(os.Stdout, "native publication fixture"); err != nil {
		t.Fatal(err)
	}
}

func executableFixture(t *testing.T) (string, []byte, []string) {
	t.Helper()
	path, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "LEVENSHTEIN_EXECUTABLE_FIXTURE=1")
	return path, data, env
}

func runExecutableFixture(ctx context.Context, path string, env []string) error {
	// Native race binaries can start slowly under concurrent build and disk
	// load. This bounds a hung fixture without asserting startup latency.
	run, err := runTool(ctx, filepath.Dir(path), []string{path, "-test.run=^TestExecutableFixture$"}, env, 30*time.Second)
	if err != nil {
		return err
	}
	if run.ExitCode != 0 || run.Stdout != executableFixtureOutput || run.Stderr != "" {
		return fmt.Errorf("native fixture: exit %d, stdout %q, stderr %q", run.ExitCode, run.Stdout, run.Stderr)
	}
	return nil
}

// Installation and unrelated forks overlap, using real native bytes rather
// than scripts that some kernels permit executing while open for writing.
func TestInstallReleasePublishesNativeExecutableDuringConcurrentStarts(t *testing.T) {
	path, binary, env := executableFixture(t)
	shared := writeReleasePin(t, releaseExample, "", "asset", binary)
	root := t.TempDir()
	dir := filepath.Join(root, "tools", "example-1.2.3")
	writeTestFile(t, filepath.Join(dir, "asset"), string(binary))

	const workers = 4
	errors := make(chan error, workers*2)
	start := make(chan struct{})
	var running sync.WaitGroup
	for range workers {
		running.Go(func() {
			<-start
			for range 8 {
				installed, err := installRelease(t.Context(), shared, root, releaseExample, "")
				if err != nil {
					errors <- err
					return
				}
				if err := runExecutableFixture(t.Context(), installed, env); err != nil {
					errors <- err
					return
				}
			}
		})
		running.Go(func() {
			<-start
			for range 16 {
				if err := runExecutableFixture(t.Context(), path, env); err != nil {
					errors <- err
					return
				}
			}
		})
	}
	close(start)
	running.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}
	installed := filepath.Join(dir, "example")
	got, err := os.ReadFile(installed)
	if err != nil || !bytes.Equal(got, binary) {
		t.Fatalf("published binary differs from reviewed bytes: %v", err)
	}
	info, err := os.Stat(installed)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("published executable permissions: %v, %v", info, err)
	}
}

func TestExecutablePublicationReleasesForkLockAfterFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "directory")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}

	if err := atomicWriteExecutable(path, []byte("cannot replace a directory")); err == nil {
		t.Fatal("publication over a directory succeeded")
	}

	if !syscall.ForkLock.TryLock() {
		t.Fatal("failed publication retained its fork reader lock")
	}
	syscall.ForkLock.Unlock()
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 || entries[0].Name() != "directory" {
		t.Fatalf("failed publication left pending files: %v, %v", entries, err)
	}
}
