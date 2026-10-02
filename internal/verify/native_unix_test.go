//go:build darwin || linux

package verify

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	t.Parallel()
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
	t.Parallel()
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

// Both streams exceed a pipe's capacity many times; retention must not stop
// the copy goroutines from draining either stream, even on concurrent checks.
func TestNativeNoisyOutputPreservesOutcomeAndCacheWarning(t *testing.T) {
	t.Parallel()
	for _, exit := range []string{"0", "7"} {
		t.Run(exit, func(t *testing.T) {
			t.Parallel()
			req := cacheRequest(t)
			req.Check.Command.Args = []string{"/bin/sh", "-c", "dd if=/dev/zero bs=1048576 count=2 2>/dev/null & dd if=/dev/zero bs=1048576 count=2 1>&2 2>/dev/null; wait; exit " + exit}
			req.Check.Command.RerunArgs = req.Check.Command.Args
			runner := CachedExecutor{Cache: &Cache{Dir: t.TempDir()}, Executor: &Native{}}

			result := runner.Execute(context.Background(), req)
			expected := StatusPassed
			if exit != "0" {
				expected = StatusFailed
			}
			if result.Status != expected || len(result.Stdout) != nativeOutputLimit || len(result.Stderr) != nativeOutputLimit || len(result.Warnings) != 2 {
				t.Fatalf("status=%s error=%s output=%d/%d warnings=%v", result.Status, result.Error, len(result.Stdout), len(result.Stderr), result.Warnings)
			}
			cached := runner.Execute(context.Background(), req)
			if (exit == "0" && cached.Cache.Status != CacheHit) || cached.Status != expected || len(cached.Warnings) != 2 || cached.Warnings[0].Kind != WarningOutputTruncated {
				t.Fatalf("cached status=%s cache=%s warnings=%v", cached.Status, cached.Cache.Status, cached.Warnings)
			}
		})
	}
}

func TestNoisyHelpersRefuseIncompleteDiagnostics(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	args := []string{"/bin/sh", "-c", "printf '{\"findings\":null}'; dd if=/dev/zero bs=1048576 count=2 2>/dev/null | tr '\\000' ' '; exit 0"}
	run, err := runTool(context.Background(), dir, args, os.Environ(), 10*time.Second)
	if err == nil || run.ExitCode != 0 || len(run.Stdout) != nativeOutputLimit || len(run.Warnings) != 1 {
		t.Fatalf("helper: exit=%d err=%v length=%d warnings=%v", run.ExitCode, err, len(run.Stdout), run.Warnings)
	}
	var completePrefix map[string]any
	if parseErr := json.Unmarshal([]byte(run.Stdout), &completePrefix); parseErr != nil {
		t.Fatalf("regression needs a parseable retained prefix: %v", parseErr)
	}
	found, invocation, err := gocheckRun(context.Background(), CheckGoImports, dir, args, os.Environ())
	if err == nil || len(found) != 0 || len(invocation.Warnings) != 1 {
		t.Fatalf("structured diagnostics: findings=%v err=%v warnings=%v", found, err, invocation.Warnings)
	}
	executor := goCheckExecutor(func(_ *Native, ctx context.Context, _ Request, work goRun) ([]finding, toolRun, error) {
		return gocheckRun(ctx, CheckGoImports, work.Dir, args, work.Env)
	}, "findings")
	result := executor(&Native{}, context.Background(), Request{}, dir, os.Environ())
	if result.Status != StatusError || len(result.Details) != 0 || len(result.Warnings) != 1 {
		t.Fatalf("executor: status=%s err=%s warnings=%v", result.Status, result.Error, result.Warnings)
	}
}

func TestNoisyOutputPreservesLifecycle(t *testing.T) {
	t.Parallel()
	for _, timeout := range []bool{false, true} {
		t.Run(strconv.FormatBool(timeout), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			reader := holdFIFO(t, dir)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if !timeout {
				time.AfterFunc(500*time.Millisecond, cancel)
			}
			result := command(ctx, dir, []string{"/bin/sh", "-c", "exec 3>hold; dd if=/dev/zero bs=1048576 count=2 2>/dev/null; sleep 30 & wait"}, os.Environ(), "1s")
			expected := StatusCancelled
			if timeout {
				expected = StatusError
			}
			if result.Status != expected || len(result.Stdout) != nativeOutputLimit || len(result.Warnings) != 1 {
				t.Fatalf("lifecycle: status=%s err=%s length=%d warnings=%v", result.Status, result.Error, len(result.Stdout), result.Warnings)
			}
			awaitWritersGone(t, reader)
		})
	}
	dir := t.TempDir()
	reader := holdFIFO(t, dir)
	result := command(context.Background(), dir, []string{"/bin/sh", "-c", "exec 3>hold; dd if=/dev/zero bs=1048576 count=2 2>/dev/null; sleep 30 & exit 0"}, os.Environ(), "5s")
	if result.Status != StatusPassed || len(result.Stdout) != nativeOutputLimit || len(result.Warnings) != 2 || result.Warnings[0].Kind != WarningDetachedOutput {
		t.Fatalf("detached: status=%s err=%s length=%d warnings=%v", result.Status, result.Error, len(result.Stdout), result.Warnings)
	}
	awaitWritersGone(t, reader)
}

func TestTruncatedToolVersionCannotMatchItsRetainedPrefix(t *testing.T) {
	t.Parallel()
	expected := strings.Repeat("x", nativeOutputLimit)
	req := Request{Environment: Environment{Tools: []Tool{{Command: []string{"/bin/sh", "-c", "dd if=/dev/zero bs=1048576 count=2 2>/dev/null | tr '\\000' x"}, Version: expected}}}}
	result := validateTools(context.Background(), t.TempDir(), req.Environment.Tools, os.Environ())
	if result == nil || result.Status != StatusError || !strings.Contains(result.Error, "output truncated") || len(result.Warnings) != 1 {
		t.Fatal("truncated version was accepted or lost its warning")
	}
}
