package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// The Dagger path pins a Go image by digest, so the container's toolchain is
// part of the implementation snapshot. The native path uses whatever Go the
// host provides, which no snapshot covers, so the toolchain has to identify the
// result itself.
type goToolchain struct {
	Version string `json:"version"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
}

// toolchains memoizes one identity per resolved go binary for the life of the
// process; a run does not change its host toolchain mid-flight.
var toolchains sync.Map

// analysisEnv runs the check against the host's installed Go, never one Go
// downloads for itself, exactly as the pinned container does. gowork is the
// workspace the container would see ("off" or a go.work path); see workspace.
func analysisEnv(env []string, gowork string) []string {
	return goEnv(env, []string{"GOTOOLCHAIN=local", "GOWORK=" + gowork})
}

// buildEnv compiles the shared checkout's own helper modules, which are never
// part of a workspace, so it ignores one if the host has it configured.
func buildEnv(env []string) []string {
	return goEnv(env, []string{"GOTOOLCHAIN=local", "GOWORK=off"})
}

func goEnv(env, pinned []string) []string {
	out := make([]string, 0, len(env)+len(pinned))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		replaced := false
		for _, pin := range pinned {
			if pinName, _, _ := strings.Cut(pin, "="); pinName == name {
				replaced = true
			}
		}
		if !replaced {
			out = append(out, entry)
		}
	}
	return append(out, pinned...)
}

func toolchainIdentity(ctx context.Context, dir string, env []string) (goToolchain, error) {
	env = buildEnv(env)
	binary, err := executable(dir, env, "go")
	if err != nil {
		return goToolchain{}, err
	}
	if memoized, ok := toolchains.Load(binary); ok {
		return memoized.(goToolchain), nil
	}

	run, err := runTool(ctx, dir, []string{binary, "env", "GOVERSION", "GOOS", "GOARCH"}, env, 30*time.Second)
	if err != nil {
		return goToolchain{}, err
	}
	if run.ExitCode != 0 {
		return goToolchain{}, fmt.Errorf("go env failed: %s", strings.TrimSpace(run.Stderr))
	}
	values := strings.Fields(run.Stdout)
	if len(values) != 3 {
		return goToolchain{}, fmt.Errorf("go env returned %q", run.Stdout)
	}

	identity := goToolchain{Version: values[0], OS: values[1], Arch: values[2]}
	toolchains.Store(binary, identity)
	return identity, nil
}

// toolRun is one helper invocation. The executor needs the exit code itself:
// for a lint tool "1" means diagnostics and anything else means the tool
// failed, and only the caller knows which is which.
type toolRun struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

func runTool(ctx context.Context, dir string, args, env []string, timeout time.Duration) (toolRun, error) {
	child, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	path, err := executable(dir, env, args[0])
	if err != nil {
		return toolRun{}, err
	}

	cmd := exec.CommandContext(child, path, args[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.WaitDelay = time.Second
	configureProcess(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err = cmd.Run()
	cleanupProcess(cmd)
	run := toolRun{Stdout: stdout.String(), Stderr: stderr.String()}

	var exit *exec.ExitError
	switch {
	case err == nil:
		return run, nil
	case ctx.Err() != nil:
		return run, ctx.Err()
	case child.Err() != nil:
		return run, fmt.Errorf("%s timed out after %s", filepath.Base(path), timeout)
	case errors.As(err, &exit):
		run.ExitCode = exit.ExitCode()
		return run, nil
	default:
		return run, err
	}
}

// helper is one build the shared checkout provides: a module directory inside
// that checkout, the package to build, and the name the binary gets.
type helper struct {
	Name   string
	Module string
	Pkg    string
}

var (
	helperLint       = helper{Name: "levenshtein-lint", Module: "runner/lint", Pkg: "./cmd/levenshtein-lint"}
	helperActionlint = helper{Name: "actionlint", Module: "runner/tools", Pkg: "github.com/rhysd/actionlint/cmd/actionlint"}
	helperVulncheck  = helper{Name: "govulncheck", Module: "runner/tools", Pkg: "golang.org/x/vuln/cmd/govulncheck"}
)

// cacheRoot is where native Go checks keep the state they own: built helpers,
// the Staticcheck analysis cache, and the locks that serialize them. Without a
// configured result cache it is a directory for this one check execution, which
// release removes; Go's build cache still makes the repeat helper build cheap.
func (n *Native) cacheRoot() (string, func(), error) {
	if n.Cache != nil && n.Cache.Dir != "" {
		return n.Cache.Dir, func() {}, nil
	}

	dir, err := os.MkdirTemp("", "levenshtein-tools-")
	if err != nil {
		return "", func() {}, err
	}
	return dir, func() { _ = os.RemoveAll(dir) }, nil
}

// build compiles one helper from the shared checkout into work.Root. Go's own
// build cache makes a repeat build cheap, so there is no staleness logic here;
// the file lock only keeps two concurrent checks from writing the same output
// path.
func build(ctx context.Context, req Request, work goRun, tool helper) (string, error) {
	module, err := contained(req.Shared, filepath.FromSlash(tool.Module))
	if err != nil {
		return "", fmt.Errorf("shared checkout has no %s: %w", tool.Module, err)
	}

	output := filepath.Join(work.Root, "tools", tool.Name)
	unlock, err := lockFile(ctx, filepath.Join(work.Root, "locks", "tool-"+tool.Name))
	if err != nil {
		return "", err
	}
	defer unlock()
	if err := os.MkdirAll(filepath.Dir(output), 0700); err != nil {
		return "", err
	}

	run, err := runTool(ctx, module, []string{"go", "build", "-trimpath", "-o", output, tool.Pkg}, buildEnv(work.Env), 10*time.Minute)
	if err != nil {
		return "", err
	}
	if run.ExitCode != 0 {
		return "", fmt.Errorf("building %s: %s", tool.Name, strings.TrimSpace(run.Stderr+run.Stdout))
	}
	return output, nil
}

// sharedChecks reads the pinned Staticcheck rule list from the shared checkout
// rather than repeating it here, so the two executors select the same rules.
func sharedChecks(shared string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(shared, "runner", "toolchain.json"))
	if err != nil {
		return nil, fmt.Errorf("shared checkout has no runner/toolchain.json: %w", err)
	}

	var tools struct {
		Checks []string `json:"checks"`
	}
	if err := json.Unmarshal(data, &tools); err != nil {
		return nil, err
	}
	if len(tools.Checks) == 0 {
		return nil, fmt.Errorf("runner/toolchain.json declares no checks")
	}
	return tools.Checks, nil
}
