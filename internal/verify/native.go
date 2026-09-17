package verify

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Native serializes mutable preparation within this runner. Cross-process reuse
// is handled by the cache layer; repo commands remain trusted, unsandboxed code.
type Native struct {
	mu       sync.Mutex
	prepared map[string][]string
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

	// Hold ownership of mutable preparation through the check that consumes it.
	n.mu.Lock()
	defer n.mu.Unlock()
	if req.Preparation != nil {
		if n.prepared == nil {
			n.prepared = map[string][]string{}
		}
		data, _ := json.Marshal(struct {
			Source      string
			Workspace   string
			Env         []string
			Preparation *Preparation
		}{req.Source, req.Target.Workspace, nativeEnv(req, req.Preparation.Env), req.Preparation})
		sum := sha256.Sum256(data)
		key := hex.EncodeToString(sum[:])
		_, known := n.prepared[key]
		ready := known && outputsExist(req.Source, req.Preparation.Outputs)
		if !ready {
			owned := make([]string, 0, len(req.Preparation.Outputs))
			for _, path := range req.Preparation.Outputs {
				full, err := outputPath(req.Source, path)
				if err != nil {
					return Result{Status: StatusError, Error: err.Error()}
				}
				owned = append(owned, full)
			}
			for prior, outputs := range n.prepared {
				for _, old := range outputs {
					for _, path := range owned {
						if old == path || strings.HasPrefix(old, path+string(filepath.Separator)) || strings.HasPrefix(path, old+string(filepath.Separator)) {
							delete(n.prepared, prior)
						}
					}
				}
			}
			if result := validateTools(ctx, filepath.Join(req.Source, req.Target.Workspace), req.Environment.Tools, nativeEnv(req, req.Preparation.Env)); result != nil {
				return *result
			}
			prep := command(ctx, filepath.Join(req.Source, req.Target.Workspace), req.Preparation.Command, nativeEnv(req, req.Preparation.Env), req.Preparation.Timeout)
			if prep.Status != StatusPassed {
				return prep.withOutcome(prep.Status, "preparation: "+prep.Error)
			}
			if !outputsExist(req.Source, req.Preparation.Outputs) {
				return Result{Status: StatusError, Error: "preparation did not produce its declared outputs"}
			}
			n.prepared[key] = owned
		}
	}

	result := command(ctx, dir, req.Check.Command, env, req.Check.Timeout)
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

func outputsExist(source string, paths []string) bool {
	for _, path := range paths {
		if _, err := outputPath(source, path); err != nil {
			return false
		}
		if _, err := contained(source, path); err != nil {
			return false
		}
	}
	return true
}

// Mutable output paths must not alias another preparation through a symlink.
func outputPath(root, path string) (string, error) {
	if !relative(path) || path == "." {
		return "", fmt.Errorf("invalid output path %q", path)
	}
	full := filepath.Join(root, path)
	for current := full; current != root; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("output path %q contains a symlink", path)
		}
	}
	return full, nil
}
