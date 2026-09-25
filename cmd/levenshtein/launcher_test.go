package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
)

// launcherRoot is the checkout holding the launcher under test.
func launcherRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// launcherGoCache keeps a launcher test that moves XDG_CACHE_HOME on Linux, where
// Go's build cache lives under it by default, from rebuilding the CLI cold.
func launcherGoCache(t *testing.T) string {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "go", "env", "GOCACHE").Output()
	if err != nil {
		t.Skipf("go is not installed: %v", err)
	}
	return strings.TrimSpace(string(out))
}

// runLauncher runs a verify launcher with exactly the given environment and
// returns its exit code and combined output.
func runLauncher(t *testing.T, launcher string, env []string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), launcher, args...)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), string(out)
	}
	if err != nil {
		t.Fatal(err)
	}
	return 0, string(out)
}

// launcherEnvWithout drops the named variables from an environment.
func launcherEnvWithout(env []string, names ...string) []string {
	return slices.DeleteFunc(slices.Clone(env), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return slices.Contains(names, name)
	})
}

// fakeLauncherRoot is a copy of the launcher beside a stand-in CLI that prints the
// platform it was built for, the Go settings it was started with, and its
// arguments, so a test can see what the build used and what the CLI received.
func fakeLauncherRoot(t *testing.T) string {
	t.Helper()
	root := launcherRoot(t)
	launcher, err := os.ReadFile(filepath.Join(root, "verify"))
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := os.ReadFile(filepath.Join(root, ".go-version"))
	if err != nil {
		t.Fatal(err)
	}

	fake := t.TempDir()
	files := map[string]string{
		".go-version": string(pinned),
		"go.mod":      "module example.com/fake\n\ngo " + strings.TrimSpace(string(pinned)) + "\n",
		"cmd/levenshtein/main.go": `package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

func main() {
	fmt.Println("platform=" + runtime.GOOS + "/" + runtime.GOARCH)
	for _, name := range []string{"GOOS", "GOARCH", "GOFLAGS", "GOEXPERIMENT", "CGO_ENABLED", "GOWORK", "GOTOOLCHAIN"} {
		fmt.Printf("%s=%s\n", name, os.Getenv(name))
	}
	fmt.Println("args=" + strings.Join(os.Args[1:], " "))
}
`,
	}
	for name, body := range files {
		path := filepath.Join(fake, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(fake, "verify"), launcher, 0o755); err != nil {
		t.Fatal(err)
	}
	return fake
}

func TestLauncherIgnoresCallerToolchain(t *testing.T) {
	root := launcherRoot(t)
	caller := t.TempDir()
	if err := os.WriteFile(filepath.Join(caller, "go.work"), []byte("go 1.99.0\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), filepath.Join(root, "verify"), "branch", "--dry-run")
	cmd.Dir = caller
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=auto", "GOWORK="+filepath.Join(caller, "go.work"), "GOPROXY=off", "XDG_CACHE_HOME="+t.TempDir(), "GOCACHE="+launcherGoCache(t))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("consumer toolchain affected launcher: %v\n%s", err, out)
	}
}

// foreignPlatform names a GOOS and GOARCH the host is not, so a binary built
// for them cannot run here.
func foreignPlatform() (string, string) {
	goos, goarch := "linux", "amd64"
	if runtime.GOOS == "linux" {
		goos = "darwin"
	}
	if runtime.GOARCH == "amd64" {
		goarch = "arm64"
	}
	return goos, goarch
}

// A shell set up to cross-compile or to build from a vendor directory must
// still get a CLI for this host, and the CLI must see that shell's settings,
// not the ones the launcher built with.
func TestLauncherBuildsForTheHostAndPassesTheCallerEnvironment(t *testing.T) {
	goos, goarch := foreignPlatform()
	caller := []string{
		"GOOS=" + goos,
		"GOARCH=" + goarch,
		"GOFLAGS=-mod=vendor",
		"GOEXPERIMENT=nosuchexperiment",
		"CGO_ENABLED=1",
		"GOTOOLCHAIN=local",
		"GOPROXY=off",
	}
	env := append(launcherEnvWithout(os.Environ(), "GOWORK"), caller...)
	env = append(env, "XDG_CACHE_HOME="+t.TempDir(), "GOCACHE="+launcherGoCache(t))

	code, out := runLauncher(t, filepath.Join(fakeLauncherRoot(t), "verify"), env, "branch", "--dry-run")

	if code != 0 {
		t.Fatalf("the launcher must build for the host whatever the caller's Go settings: exit %d\n%s", code, out)
	}
	want := []string{
		"platform=" + runtime.GOOS + "/" + runtime.GOARCH,
		"GOOS=" + goos,
		"GOARCH=" + goarch,
		"GOFLAGS=-mod=vendor",
		"GOEXPERIMENT=nosuchexperiment",
		"CGO_ENABLED=1",
		"GOWORK=",
		"GOTOOLCHAIN=local",
		"args=--shared ",
	}
	for _, line := range want {
		if !strings.Contains(out, line) {
			t.Errorf("CLI output lacks %q:\n%s", line, out)
		}
	}
	if !strings.Contains(out, "branch --dry-run") {
		t.Errorf("CLI arguments lost the caller's:\n%s", out)
	}

	// The real CLI builds with the same settings, including -mod=vendor
	// in a module that has no vendor directory.
	code, out = runLauncher(t, filepath.Join(launcherRoot(t), "verify"), env, "branch", "--dry-run")

	if code != 0 {
		t.Fatalf("the real launcher must build for the host: exit %d\n%s", code, out)
	}
}

// Launchers started together share one binary path. Each builds to its own
// temporary name and renames it into place, so every one of them runs a whole
// binary and none leaves a temporary file behind.
func TestConcurrentLaunchersShareTheBinary(t *testing.T) {
	root := fakeLauncherRoot(t)
	cache := t.TempDir()
	env := append(launcherEnvWithout(os.Environ(), "GOOS", "GOARCH", "GOFLAGS", "GOEXPERIMENT"), "XDG_CACHE_HOME="+cache, "GOCACHE="+launcherGoCache(t), "GOPROXY=off")

	// Only the test's own goroutine may stop it, so the launchers report back
	// instead of calling runLauncher.
	var wg sync.WaitGroup
	errs := make([]error, 6)
	outputs := make([][]byte, len(errs))
	for i := range errs {
		wg.Go(func() {
			cmd := exec.CommandContext(t.Context(), filepath.Join(root, "verify"), "branch")
			cmd.Env = env
			outputs[i], errs[i] = cmd.CombinedOutput()
		})
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil || !strings.Contains(string(outputs[i]), "args=--shared ") {
			t.Errorf("launcher %d: %v\n%s", i, err, outputs[i])
		}
	}
	bins, err := filepath.Glob(filepath.Join(cache, "levenshtein", "bin", "*", "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(bins) != 1 || filepath.Base(bins[0]) != "levenshtein" {
		t.Fatalf("the cache must hold only the installed binary: %v", bins)
	}
}

// Exit 1 means a check failed, and the Claude Stop hook blocks the agent on
// it, so every way the launcher can fail before the CLI runs is exit 2.
func TestLauncherSetupFailuresExitTwo(t *testing.T) {
	notADirectory := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(notADirectory, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	unreadable := fakeLauncherRoot(t)
	if err := os.Remove(filepath.Join(unreadable, ".go-version")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(unreadable, ".go-version"), 0o755); err != nil {
		t.Fatal(err)
	}
	empty := fakeLauncherRoot(t)
	if err := os.WriteFile(filepath.Join(empty, ".go-version"), []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	broken := fakeLauncherRoot(t)
	if err := os.WriteFile(filepath.Join(broken, "cmd", "levenshtein", "main.go"), []byte("package main\n\nfunc main() {"), 0o600); err != nil {
		t.Fatal(err)
	}
	base := append(launcherEnvWithout(os.Environ(), "XDG_CACHE_HOME"), "GOCACHE="+launcherGoCache(t), "GOPROXY=off")

	for name, tc := range map[string]struct {
		root string
		env  []string
		want string
	}{
		"no HOME or XDG_CACHE_HOME": {
			root: fakeLauncherRoot(t),
			env:  launcherEnvWithout(base, "HOME"),
			want: "Set HOME or XDG_CACHE_HOME",
		},
		"a cache directory that cannot be created": {
			root: fakeLauncherRoot(t),
			env:  append(slices.Clone(base), "XDG_CACHE_HOME="+notADirectory),
			want: "Cannot create the launcher's cache directory",
		},
		"an unreadable .go-version": {
			root: unreadable,
			env:  append(slices.Clone(base), "XDG_CACHE_HOME="+t.TempDir()),
			want: ".go-version",
		},
		"an empty .go-version": {
			root: empty,
			env:  append(slices.Clone(base), "XDG_CACHE_HOME="+t.TempDir()),
			want: ".go-version is empty",
		},
		"a CLI that does not build": {
			root: broken,
			env:  append(slices.Clone(base), "XDG_CACHE_HOME="+t.TempDir()),
			want: "Building the Levenshtein CLI failed",
		},
	} {
		t.Run(name, func(t *testing.T) {
			code, out := runLauncher(t, filepath.Join(tc.root, "verify"), tc.env, "branch")

			if code != 2 || !strings.Contains(out, tc.want) {
				t.Fatalf("want exit 2 naming %q, got exit %d:\n%s", tc.want, code, out)
			}
		})
	}
}

// The launcher asks Go for the pinned toolchain, so the only Go requirement it
// states is that some go exists. The message names the pinned version and the
// document that explains how to obtain it.
func TestLauncherNeedsOnlyAGoOnPath(t *testing.T) {
	root := launcherRoot(t)
	pinned, err := os.ReadFile(filepath.Join(root, ".go-version"))
	if err != nil {
		t.Fatal(err)
	}

	// A PATH holding the launcher's shell utilities and no go at all. Ordinary
	// system directories cannot stand in for it: a CI image may install Go there.
	bin := t.TempDir()
	for _, tool := range []string{"bash", "dirname", "cksum", "awk", "mkdir"} {
		installed, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("the launcher's %s is not installed: %v", tool, err)
		}
		if err := os.Symlink(installed, filepath.Join(bin, tool)); err != nil {
			t.Fatal(err)
		}
	}

	code, out := runLauncher(t, filepath.Join(root, "verify"), []string{"PATH=" + bin, "HOME=" + t.TempDir()}, "branch", "--dry-run")

	if code != 2 {
		t.Fatalf("launcher without a Go on PATH: exit %d, want 2:\n%s", code, out)
	}
	if !strings.Contains(out, strings.TrimSpace(string(pinned))) || !strings.Contains(out, "docs/setup.md") {
		t.Fatalf("message names neither the pinned version nor setup:\n%s", out)
	}
}
