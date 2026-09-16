//go:build darwin || linux

package verify

import (
	"os"
	"path/filepath"

	"github.com/google/renameio/v2"
)

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer root.Close()
	return atomicWriteRoot(root, filepath.Base(path), data, mode)
}

func atomicWriteRoot(root *os.Root, path string, data []byte, mode os.FileMode) error {
	if err := root.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	// WriteFile preserves an existing destination's permissions. Artifact restore
	// must instead restore the recorded mode, so use the library's pending file.
	pending, err := renameio.NewPendingFile(path, renameio.WithRoot(root), renameio.WithStaticPermissions(mode))
	if err != nil {
		return err
	}
	defer pending.Cleanup()
	if _, err := pending.Write(data); err != nil {
		return err
	}
	return pending.CloseAtomicallyReplace()
}
