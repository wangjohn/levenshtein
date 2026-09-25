package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestCommandPlanningHelpAndErrorsNeedNoExecutor(t *testing.T) {
	t.Setenv("LEVENSHTEIN_SHARED_ROOT", "")
	t.Setenv("PATH", "")
	source := t.TempDir()
	for _, tc := range []struct {
		name    string
		args    []string
		code    int
		message string
	}{
		{"plan", []string{"--source", source, "branch", "--dry-run"}, 0, ""},
		{"help", []string{"--help"}, 0, "Usage: verify"},
		{"unknown flag", []string{"--unknown"}, 2, "unknown flag"},
		{"missing run", []string{"--source", source, "unknown", "--dry-run"}, 2, "missing or empty"},
		{"missing shared", []string{"--source", source}, 2, "set --shared"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			code, err := runCommand(context.Background(), tc.args, console{out: &output, err: io.Discard})
			if code != tc.code {
				t.Fatalf("code=%d error=%v", code, err)
			}
			if code == 2 {
				if err == nil || !strings.Contains(err.Error(), tc.message) || output.Len() != 0 {
					t.Fatalf("error=%v output=%s", err, &output)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.name == "plan" {
				if !json.Valid(output.Bytes()) {
					t.Fatalf("invalid plan JSON: %s", &output)
				}
			} else if !strings.Contains(output.String(), tc.message) {
				t.Fatalf("help: %s", &output)
			}
		})
	}
	if code, err := runCommand(context.Background(), []string{"--source", source, "--dry-run"}, console{out: failingOutput{}, err: io.Discard}); code != 2 || err == nil {
		t.Fatalf("lost output error: code=%d error=%v", code, err)
	}
}

type failingOutput struct{}

func (failingOutput) Write([]byte) (int, error) { return 0, errors.New("output unavailable") }

// A cache directory inside the shared checkout is rejected however the
// checkout is spelled: through a symlink, and before the directory exists.
func TestCacheDirInsideSymlinkedSharedCheckoutIsRejected(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "target")
	link := filepath.Join(base, "link")
	source := filepath.Join(base, "src")
	for _, dir := range []string{target, source} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	for _, shared := range []string{target, link} {
		for _, cache := range []string{filepath.Join(target, "cache", "deep"), filepath.Join(link, "cache", "deep")} {
			code, err := runCommand(context.Background(), []string{"branch", "--source", source, "--shared", shared, "--cache-dir", cache}, console{out: io.Discard, err: io.Discard})
			if code != 2 || err == nil || !strings.Contains(err.Error(), "cache directory must be outside") {
				t.Fatalf("shared %s cache %s: code %d err %v", shared, cache, code, err)
			}
		}
	}

	code, err := runCommand(context.Background(), []string{"branch", "--source", source, "--shared", filepath.Join(base, "absent")}, console{out: io.Discard, err: io.Discard})
	if code != 2 || err == nil || !strings.Contains(err.Error(), "--shared") {
		t.Fatalf("missing shared checkout: code %d err %v", code, err)
	}
}

// commandConfig writes a configuration whose one native check runs args,
// with extra top-level fields spliced in, and returns the source directory.
func commandConfig(t *testing.T, args, extra string) string {
	t.Helper()
	source := t.TempDir()
	body := `{"version": 1, "targets": {"app": {"dir": ".", "inputs": ["."]}}, "environments": {"host": {"executor": "native"}},
  "checks": {"probe": {"kind": "command", "target": "app", "environment": "host", "command": {"args": ` + args + `}}}, "runs": {"branch": {"checks": ["probe"]}}` + extra + `}`
	if err := os.WriteFile(filepath.Join(source, "levenshtein.json"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return source
}

func sharedCheckout(t *testing.T) string {
	t.Helper()
	shared, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return shared
}

// A run the caller cancelled reached no verdict, so it exits 1, never 0.
func TestCancelledRunExitsOne(t *testing.T) {
	source := commandConfig(t, `["true"]`, "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var output bytes.Buffer
	code, err := runCommand(ctx, []string{"branch", "--source", source, "--shared", sharedCheckout(t), "--cache-dir", t.TempDir()}, console{out: &output, err: io.Discard})

	if code != 1 || err != nil || !strings.Contains(output.String(), `"cancelled"`) {
		t.Fatalf("code %d, err %v, report %s", code, err, &output)
	}
}

// --write-baseline records a finished run whatever it found, but a check that
// reached no verdict exits 1 and leaves the baseline file unwritten.
func TestWriteBaselineExitsOneWithoutAVerdict(t *testing.T) {
	source := commandConfig(t, `["levenshtein-test-tool-that-does-not-exist"]`, `, "baseline": ".levenshtein/baseline.json"`)

	var notes bytes.Buffer
	code, err := runCommand(context.Background(), []string{"branch", "--source", source, "--shared", sharedCheckout(t), "--cache-dir", t.TempDir(), "--write-baseline"}, console{out: io.Discard, err: &notes})

	if code != 1 || err == nil {
		t.Fatalf("code %d, err %v, notes %s", code, err, &notes)
	}
	if _, statErr := os.Stat(filepath.Join(source, ".levenshtein", "baseline.json")); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("the baseline must not be written without a verdict: %v", statErr)
	}
}

// The cache may not live in the source checkout either, where a run would
// fingerprint its own cache.
func TestCacheDirInsideSourceCheckoutIsRejected(t *testing.T) {
	source := commandConfig(t, `["true"]`, "")

	code, err := runCommand(context.Background(), []string{"branch", "--source", source, "--shared", sharedCheckout(t), "--cache-dir", filepath.Join(source, "cache")}, console{out: io.Discard, err: io.Discard})

	if code != 2 || err == nil || !strings.Contains(err.Error(), "cache directory must be outside") {
		t.Fatalf("code %d, err %v", code, err)
	}
}

// The first interrupt cancels the run; the handler is then released, so a
// second one ends a teardown that hangs instead of being swallowed. The
// helper process stands in for run: it hangs after its context ends.
func TestSecondInterruptEndsAHungTeardown(t *testing.T) {
	if os.Getenv("LEVENSHTEIN_INTERRUPT_HELPER") == "1" {
		ctx, stop := interruptible()
		defer stop()
		fmt.Println("ready")
		<-ctx.Done()
		fmt.Println("cancelled")
		select {}
	}
	if runtime.GOOS == "windows" {
		t.Skip("interrupts are not delivered as signals on Windows")
	}
	// A process started with interrupts ignored, as a background job or under
	// nohup is, passes that on: once released, the helper ignores the second
	// interrupt too, which is right for it and not what this test measures.
	if signal.Ignored(os.Interrupt) {
		t.Skip("interrupts are ignored in this process and its children")
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSecondInterruptEndsAHungTeardown$")
	cmd.Env = append(os.Environ(), "LEVENSHTEIN_INTERRUPT_HELPER=1")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := bufio.NewScanner(stdout)
	expect := func(want string) {
		t.Helper()
		if !lines.Scan() || lines.Text() != want {
			t.Fatalf("helper printed %q, want %q (%v)", lines.Text(), want, lines.Err())
		}
	}

	expect("ready")
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	expect("cancelled")
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	// Keep interrupting: the handler is released just after the context ends.
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.After(10 * time.Second)
	for {
		select {
		case err := <-exited:
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.Success() {
				t.Fatalf("the second interrupt must end the process: %v", err)
			}
			return
		case <-ticker.C:
			_ = cmd.Process.Signal(os.Interrupt) // The process may already be gone.
		case <-deadline:
			_ = cmd.Process.Kill()
			t.Fatal("a second interrupt was swallowed while teardown hung")
		}
	}
}
