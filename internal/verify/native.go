package verify

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
)

// Native serializes mutable preparation within this runner. Cross-process reuse
// is handled by the cache layer; repo commands remain trusted, unsandboxed code.
type Native struct {
	mu     sync.Mutex
	stages map[string]stageEntry
	Cache  *Cache
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
	for _, item := range req.stages() {
		info, failure := n.stage(ctx, req, item.kind, item.definition)
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
