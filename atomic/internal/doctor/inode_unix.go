//go:build !windows

package doctor

import (
	"os"
	"syscall"
)

// inodeKey returns a unique uint64 key for an os.FileInfo suitable for
// deduplicating files by inode on unix-like systems.
func inodeKey(info os.FileInfo) uint64 {
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		// Fallback: use size+modtime as a weak key.
		return uint64(info.Size()) ^ uint64(info.ModTime().UnixNano())
	}
	// Combine device and inode for a globally unique key.
	return uint64(sys.Dev)<<32 | uint64(sys.Ino)
}
