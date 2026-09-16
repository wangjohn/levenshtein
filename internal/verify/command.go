package verify

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

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
	duration := 5 * time.Minute
	if timeout != "" {
		var err error
		duration, err = time.ParseDuration(timeout)
		if err != nil || duration <= 0 {
			return Result{Status: StatusError, Error: "invalid timeout"}
		}
	}

	child, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	path, err := executable(dir, env, args[0])
	if err != nil {
		return Result{Status: StatusError, Error: err.Error()}
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

	status := StatusError
	message := ""
	switch {
	case ctx.Err() != nil:
		status, message = StatusCancelled, ctx.Err().Error()
	case child.Err() != nil:
		message = "command timed out"
	case err == nil:
		status = StatusPassed
	default:
		if _, ok := err.(*exec.ExitError); ok {
			status = StatusFailed
		}
		message = err.Error()
	}

	return Result{Status: status, Error: message, Stdout: stdout.String(), Stderr: stderr.String()}
}

func validateTools(ctx context.Context, dir string, tools []Tool, env []string) *Result {
	for _, tool := range tools {
		result := command(ctx, dir, tool.Command, env, "30s")
		if result.Status != StatusPassed {
			failure := result.withOutcome(StatusError, "tool validation: "+result.Error)
			return &failure
		}
		if strings.TrimSpace(result.Stdout) != tool.Version {
			return &Result{Status: StatusError, Error: fmt.Sprintf("tool %q version mismatch: expected %q, got %q", tool.Command[0], tool.Version, strings.TrimSpace(result.Stdout))}
		}
	}

	return nil
}
