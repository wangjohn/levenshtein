package verify

import (
	"fmt"
	"os"
	"path/filepath"
)

// Mutable output paths must not alias another preparation through a symlink.
func outputPath(root, path string) (string, error) {
	if !relative(path) || path == "." {
		return "", fmt.Errorf("invalid output path %q", path)
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return "", err
	}
	defer dir.Close()
	for current := path; current != "."; current = filepath.Dir(current) {
		info, err := dir.Lstat(current)
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
	return filepath.Join(root, path), nil
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
