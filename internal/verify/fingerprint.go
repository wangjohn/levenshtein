package verify

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

func digest(value any) string {
	data, _ := json.Marshal(value)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func excluded(path string, excludes []string) bool {
	if filepath.Base(path) == ".git" {
		return true
	}
	for _, p := range excludes {
		if path == p || strings.HasPrefix(path, p+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

// Snapshot hashes content, names and modes, including untracked files and missing
// paths. Source symlinks disable result reuse; output snapshots retain link text.
func snapshot(root string, paths, excludes []string, outputs bool) (string, error) {
	entries := map[string]string{}
	for _, path := range paths {
		if !relative(path) {
			return "", fmt.Errorf("invalid input %q", path)
		}
		err := filepath.WalkDir(filepath.Join(root, path), func(full string, entry fs.DirEntry, err error) error {
			rel, relErr := filepath.Rel(root, full)
			if relErr != nil {
				return relErr
			}
			if excluded(rel, excludes) {
				if entry != nil && entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if os.IsNotExist(err) {
				entries[rel] = "missing"
				return nil
			}
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				if !outputs {
					return fmt.Errorf("source symlink %q requires fresh execution", rel)
				}
				link, err := os.Readlink(full)
				if err != nil {
					return err
				}
				entries[rel] = digest([]string{"symlink", link})
				return nil
			}
			if info.IsDir() {
				entries[rel] = "directory"
				return nil
			}
			if !info.Mode().IsRegular() {
				return fmt.Errorf("unsupported input file %q", rel)
			}
			file, err := os.Open(full)
			if err != nil {
				return err
			}
			h := sha256.New()
			_, readErr := io.Copy(h, file)
			closeErr := file.Close()
			if readErr != nil {
				return readErr
			}
			if closeErr != nil {
				return closeErr
			}
			entries[rel] = fmt.Sprintf("%o:%x", info.Mode().Perm(), h.Sum(nil))
			return nil
		})
		if err != nil {
			return "", err
		}
	}
	return digest(entries), nil
}
func outputPaths(req Request) []string {
	out := append([]string{}, req.Check.Artifacts...)
	if req.Preparation != nil {
		out = append(out, req.Preparation.Outputs...)
	}
	if req.Build != nil {
		out = append(out, req.Build.Outputs...)
	}
	return out
}
func implementation(req Request) (string, error) {
	paths := []string{"go.mod", "go.sum", "cmd", "internal"}
	if req.Environment.Executor == "dagger" {
		paths = append(paths, ".dagger-version", "dagger.json", "runner/main.go", "runner/config.go", "runner/toolchain.json", "runner/go.mod", "runner/go.sum")
		if req.Check.Kind == "self-test" {
			paths = append(paths, "runner/testdata")
		}
	}
	return snapshot(req.Shared, paths, nil, false)
}
func fingerprint(req Request) (string, error) {
	paths := append([]string{}, req.Target.Inputs...)
	if req.Preparation != nil {
		paths = append(paths, req.Preparation.Inputs...)
	}
	if req.Build != nil {
		paths = append(paths, req.Build.Inputs...)
	}
	sort.Strings(paths)
	source, err := snapshot(req.Source, paths, outputPaths(req), false)
	if err != nil {
		return "", err
	}
	impl, err := implementation(req)
	if err != nil {
		return "", err
	}
	req.Fresh = false
	var env []string
	if req.Environment.Executor == "native" {
		env = nativeEnv(req, req.Check.Env)
	}
	return digest(struct {
		Check                            PlannedCheck
		Source, Implementation, OS, Arch string
		Env                              []string
	}{req.PlannedCheck, source, impl, runtime.GOOS, runtime.GOARCH, env}), nil
}
