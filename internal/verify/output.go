package verify

import (
	"fmt"
	"os"
	"path/filepath"
)

// checkOutputPath rejects a mutable output path that could alias another
// preparation through a symlink.
func checkOutputPath(root, path string) error {
	if !relative(path) || path == "." {
		return fmt.Errorf("invalid output path %q", path)
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }() // Directory handle cleanup; writes are closed separately.

	for current := path; current != "."; current = filepath.Dir(current) {
		info, err := dir.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output path %q contains a symlink", path)
		}
	}
	return nil
}

func outputsExist(source string, paths []string) bool {
	for _, path := range paths {
		if err := checkOutputPath(source, path); err != nil {
			return false
		}
		if _, err := contained(source, path); err != nil {
			return false
		}
	}
	return true
}
