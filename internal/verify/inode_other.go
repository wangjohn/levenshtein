//go:build !darwin && !linux

package verify

import "io/fs"

// Platforms without a reported inode fall back to size, modification time and
// mode alone.
func inodeOf(fs.FileInfo) uint64 { return 0 }
