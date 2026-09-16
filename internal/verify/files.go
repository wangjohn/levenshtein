package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Inputs are clean, literal repository-relative paths, not globs.
func relative(path string) bool {
	return filepath.IsLocal(path) && filepath.Clean(path) == path && !strings.Contains(path, "\\")
}

func contained(root, path string) (string, error) {
	if !relative(path) {
		return "", fmt.Errorf("path %q must be a clean repository-relative path", path)
	}
	// os.Root accepts relative symlinks that stay inside the root. Absolute
	// symlinks are rejected, including aliases to another path inside this repo.
	dir, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	if _, err := dir.Stat(path); err != nil {
		return "", fmt.Errorf("path %q is unavailable or escapes source repository: %w", path, err)
	}
	// Commands need a filesystem path; native execution runs trusted repo code.
	return filepath.EvalSymlinks(filepath.Join(root, path))
}
