package verify

import (
	"bytes"
	"context"

	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Native serializes mutable preparation within this runner. Cross-process reuse
// is handled by the cache layer; repo commands remain trusted, unsandboxed code.
type Native struct {
	mu     sync.Mutex
	stages map[string]stageEntry
	Cache  *Cache
}

func nativeEnv(req Request, extra map[string]string) []string {
	values := map[string]string{}
	for _, key := range []string{"PATH", "HOME", "TMPDIR", "TMP", "TEMP", "SystemRoot"} {
		if value, ok := os.LookupEnv(key); ok {
			values[key] = value
		}
	}
	values["LANG"] = "C"
	for _, key := range req.Environment.PassEnv {
		if value, ok := os.LookupEnv(key); ok {
			values[key] = value
		}
	}
	for key, value := range req.Environment.Env {
		values[key] = value
	}
	for key, value := range extra {
		values[key] = value
	}
	values["LEVENSHTEIN_SOURCE"] = req.Source
	values["LEVENSHTEIN_WORKSPACE"] = filepath.Join(req.Source, req.Target.Workspace)
	values["LEVENSHTEIN_FRESH"] = fmt.Sprint(req.Fresh)
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env
}
func executable(dir string, env []string, name string) (string, error) {
	if strings.ContainsAny(name, "/\\") {
		if !filepath.IsAbs(name) {
			name = filepath.Join(dir, name)
		}
		return name, nil
	}
	path := ""
	for _, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			path = strings.TrimPrefix(entry, "PATH=")
		}
	}
	for _, entry := range filepath.SplitList(path) {
		if !filepath.IsAbs(entry) {
			entry = filepath.Join(dir, entry)
		}
		candidate := filepath.Join(entry, name)
		info, err := os.Stat(candidate)
		if err == nil && info.Mode().IsRegular() && info.Mode()&0111 != 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("executable %q not found in configured PATH", name)
}
func command(ctx context.Context, dir string, args, env []string, timeout string) Result {
	r := Result{Status: "error"}
	duration := 5 * time.Minute
	if timeout != "" {
		var err error
		duration, err = time.ParseDuration(timeout)
		if err != nil || duration <= 0 {
			r.Error = "invalid timeout"
			return r
		}
	}
	child, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	path, err := executable(dir, env, args[0])
	if err != nil {
		r.Error = err.Error()
		return r
	}
	cmd := exec.CommandContext(child, path, args[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.WaitDelay = time.Second
	configureProcess(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	cleanupProcess(cmd)
	r.Stdout = stdout.String()
	r.Stderr = stderr.String()
	switch {
	case ctx.Err() != nil:
		r.Status = "cancelled"
		r.Error = ctx.Err().Error()
	case child.Err() != nil:
		r.Error = "command timed out"
	case err == nil:
		r.Status = "passed"
	default:
		if _, ok := err.(*exec.ExitError); ok {
			r.Status = "failed"
		}
		r.Error = err.Error()
	}
	return r
}
func (n *Native) Execute(ctx context.Context, req Request) Result {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return Result{Status: "error", Error: "native execution currently supports macOS and Linux"}
	}
	dir, err := contained(req.Source, req.Target.Dir)
	if err != nil {
		return Result{Status: "error", Error: err.Error()}
	}
	env := nativeEnv(req, req.Check.Env)
	if result := validateTools(ctx, dir, req.Environment.Tools, env); result != nil {
		return *result
	}
	// Hold ownership of mutable preparation through the check that consumes it.
	n.mu.Lock()
	defer n.mu.Unlock()
	stages := []StageResult{}
	for _, item := range []struct {
		kind  string
		stage *Preparation
	}{{"preparation", req.Preparation}, {"build", req.Build}} {
		if item.stage == nil {
			continue
		}
		info, failure := n.stage(ctx, req, item.kind, item.stage)
		stages = append(stages, info)
		if failure != nil {
			failure.Stages = stages
			return *failure
		}
	}
	args := req.Check.Command
	if req.Fresh && len(req.Check.FreshCommand) > 0 {
		args = req.Check.FreshCommand
	}
	result := command(ctx, dir, args, env, req.Check.Timeout)
	result.Stages = stages
	if result.Status == "passed" {
		for _, path := range req.Check.Artifacts {
			full, err := contained(req.Source, path)
			if err != nil {
				result.Status = "error"
				result.Error = fmt.Sprintf("required artifact %q: %v", path, err)
				return result
			}
			info, err := os.Lstat(full)
			if err != nil || !info.Mode().IsRegular() {
				result.Status = "error"
				result.Error = fmt.Sprintf("required artifact %q must be a regular file", path)
				return result
			}
		}
	}
	return result
}
func outputsExist(source string, paths []string) bool {
	for _, path := range paths {
		if _, err := contained(source, path); err != nil {
			return false
		}
	}
	return true
}

func validateTools(ctx context.Context, dir string, tools []Tool, env []string) *Result {
	for _, tool := range tools {
		result := command(ctx, dir, tool.Command, env, "30s")
		if result.Status != "passed" {
			result.Error = "tool validation: " + result.Error
			result.Status = "error"
			return &result
		}
		if strings.TrimSpace(result.Stdout) != tool.Version {
			return &Result{Status: "error", Error: fmt.Sprintf("tool %q version mismatch: expected %q, got %q", tool.Command[0], tool.Version, strings.TrimSpace(result.Stdout))}
		}
	}

	return nil
}
