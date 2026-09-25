//go:build darwin || linux

package verify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// holdFIFO makes a FIFO in dir and opens its read end, which reads end of file
// only once every process holding its write end has exited. The read end is
// nonblocking, so opening it does not wait for a writer and a read with a
// writer still present returns EAGAIN rather than blocking.
func holdFIFO(t *testing.T, dir string) int {
	t.Helper()
	path := filepath.Join(dir, "hold")
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(fd) })
	return fd
}

// awaitWritersGone fails unless every writer of the FIFO exits within a bound
// generous enough for a slow machine; it returns as soon as they have.
func awaitWritersGone(t *testing.T, fd int) {
	t.Helper()
	buffer := make([]byte, 64)
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		n, err := syscall.Read(fd, buffer)
		if err == nil && n == 0 {
			return
		}
		if err != nil && !errors.Is(err, syscall.EAGAIN) {
			t.Fatal(err)
		}
	}
	t.Fatal("a descendant of the command is still running")
}

// The shell hands the FIFO to a background descendant and says so before it
// blocks; the kill must then reach that descendant, which still holds it.
func TestNativeTimeoutKillsProcessGroup(t *testing.T) {
	req := nativeRequest(t)
	reader := holdFIFO(t, req.Source)
	req.Check.Command.Args = []string{"/bin/sh", "-c", "exec 3>hold; sleep 30 & exec 3>&-; touch started; wait"}
	req.Check.Command.Timeout = "1s"

	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusError || result.Error != "command timed out" {
		t.Fatalf("timeout: %+v", result)
	}
	if _, err := os.Stat(filepath.Join(req.Source, "started")); err != nil {
		t.Fatalf("the command timed out before it started its descendant: %v", err)
	}
	awaitWritersGone(t, reader)
}

// A command that exits 0 but leaves a background process holding its output,
// as a daemonizing build tool can, passed. The report says what happened, and
// the straggler is killed with the rest of the process group.
func TestNativeBackgroundOutputHolderStillPasses(t *testing.T) {
	req := nativeRequest(t)
	reader := holdFIFO(t, req.Source)
	req.Check.Command.Args = []string{"/bin/sh", "-c", "exec 3>hold; sleep 30 & exec 3>&-; echo ok"}

	result := (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusPassed || result.Stdout != "ok\n" {
		t.Fatalf("a passing command with a background process was not a pass: %+v", result)
	}
	if len(result.Warnings) != 1 || result.Warnings[0].Kind != WarningDetachedOutput {
		t.Fatalf("the pass did not report the process that held its output: %+v", result.Warnings)
	}
	awaitWritersGone(t, reader)

	req.Check.Command.Args = []string{"/bin/sh", "-c", "sleep 30 & exit 3"}
	if result := (&Native{}).Execute(context.Background(), req); result.Status != StatusFailed {
		t.Fatalf("a failing command with a background process: %+v", result)
	}

	// A preparation stage is the likeliest place for a daemonizing tool, and
	// its warning belongs to the check it prepared.
	req.Preparation = &Preparation{Command: []string{"/bin/sh", "-c", "sleep 30 & touch ready"}, Inputs: []string{"lock"}, Outputs: []string{"ready"}}
	req.Check.Command.Args = []string{"true"}
	result = (&Native{}).Execute(context.Background(), req)
	if result.Status != StatusPassed || len(result.Warnings) != 1 || result.Warnings[0].Kind != WarningDetachedOutput {
		t.Fatalf("a preparation with a background process: %+v", result)
	}
}
