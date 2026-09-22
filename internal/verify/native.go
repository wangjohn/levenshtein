package verify

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sync"
)

// Native serializes mutable preparation within this runner. Reuse of stage
// outputs, in-process and across processes alike, is handled by the cache
// layer; repo commands remain trusted, unsandboxed code.
type Native struct {
	mu    sync.Mutex
	Cache *Cache
}

func (n *Native) Execute(ctx context.Context, req Request) Result {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		return Result{Status: StatusError, Error: "native execution currently supports macOS and Linux"}
	}
	dir, err := contained(req.Source, req.Target.Dir)
	if err != nil {
		return Result{Status: StatusError, Error: err.Error()}
	}

	env := nativeEnv(req, req.Check.Env)
	if result := validateTools(ctx, dir, req.Environment.Tools, env); result != nil {
		return *result
	}
	if req.Check.Kind == CheckSemanticLint {
		return semanticLint(ctx, req, dir, env)
	}

	// Hold ownership of mutable preparation through the check that consumes it.
	n.mu.Lock()
	defer n.mu.Unlock()
	stages := []StageResult{}
	for _, item := range req.stages() {
		info, failure := n.stage(ctx, req, item.kind, item.definition)
		stages = append(stages, info)
		if failure != nil {
			return failure.withStages(stages)
		}
	}

	args := req.Check.Command
	if req.RerunChecks && len(req.Check.RerunCommand) > 0 {
		args = req.Check.RerunCommand
	}

	result := command(ctx, dir, args, env, req.Check.Timeout).withStages(stages)
	if result.Status == StatusPassed {
		for _, path := range req.Check.Artifacts {
			full, err := contained(req.Source, path)
			if err != nil {
				return result.withOutcome(StatusError, fmt.Sprintf("required artifact %q: %v", path, err))
			}
			info, err := os.Lstat(full)
			if err != nil || !info.Mode().IsRegular() {
				return result.withOutcome(StatusError, fmt.Sprintf("required artifact %q must be a regular file", path))
			}
		}
	}
	return result
}
