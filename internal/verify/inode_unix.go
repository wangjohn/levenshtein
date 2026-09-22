//go:build darwin || linux

package verify

import (
	"io/fs"
	"syscall"
)

// inodeOf reports the file's inode so a replaced file is never mistaken for an
// edited one that happens to keep its size and modification time.
func inodeOf(info fs.FileInfo) uint64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	return uint64(stat.Ino)
}
