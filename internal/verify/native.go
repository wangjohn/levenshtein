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
	kind, ok := nativeKinds[req.Check.Kind]
	if !ok {
		return Result{Status: StatusError, Error: fmt.Sprintf("native executor runs %s checks, not %q", nativeKindNames(), req.Check.Kind)}
	}
	dir, err := contained(req.Source, req.Target.Dir)
	if err != nil {
		return Result{Status: StatusError, Error: err.Error()}
	}

	env := nativeEnv(req, req.Check.env())
	if result := validateTools(ctx, dir, req.Environment.Tools, env); result != nil {
		return *result
	}
	return kind.execute(n, ctx, req, dir, env)
}

// runCommand executes a command check: its preparation and build stages, the
// command itself or its fresh-run variant, and the required artifacts.
func (n *Native) runCommand(ctx context.Context, req Request, dir string, env []string) Result {
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

	options := req.Check.Command
	args := options.Args
	if req.RerunChecks && len(options.RerunArgs) > 0 {
		args = options.RerunArgs
	}

	result := command(ctx, dir, args, env, options.Timeout).withStages(stages)
	if result.Status == StatusPassed {
		for _, path := range options.Artifacts {
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
