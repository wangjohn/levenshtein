//go:build darwin || linux

package verify

import (
	"os"
	"path/filepath"
	"syscall"

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
	defer func() { _ = root.Close() }() // Directory handle cleanup; writes are closed separately.
	return atomicWriteRoot(root, filepath.Base(path), data, mode)
}

func atomicWriteRoot(root *os.Root, path string, data []byte, mode os.FileMode) error {
	if err := root.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return atomicReplace(root, path, data, mode)
}

// atomicWriteExecutable prevents another check's fork from inheriting the
// pending binary's writable descriptor. Close-on-exec alone leaves that writer
// open in the child until exec, even after the parent closes and publishes it.
func atomicWriteExecutable(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }() // Directory handle cleanup; writes are closed separately.

	syscall.ForkLock.RLock()
	defer syscall.ForkLock.RUnlock()
	return atomicReplace(root, filepath.Base(path), data, 0700)
}

func atomicReplace(root *os.Root, path string, data []byte, mode os.FileMode) error {
	// WriteFile preserves an existing destination's permissions. Artifact restore
	// must instead restore the recorded mode, so use the library's pending file.
	pending, err := renameio.NewPendingFile(path, renameio.WithRoot(root), renameio.WithStaticPermissions(mode))
	if err != nil {
		return err
	}
	defer func() { _ = pending.Cleanup() }() // Best-effort cleanup; atomic write errors are returned.
	if _, err := pending.Write(data); err != nil {
		return err
	}
	return pending.CloseAtomicallyReplace()
}
