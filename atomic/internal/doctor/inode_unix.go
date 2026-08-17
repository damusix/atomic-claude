//go:build !windows

package doctor

import (
	"os"
	"syscall"
)

// inodeKey returns a device+inode identity key for deduplicating files.
func inodeKey(info os.FileInfo) uint64 {
	sys, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		// Weak fallback key.
		return uint64(info.Size()) ^ uint64(info.ModTime().UnixNano())
	}
	return uint64(sys.Dev)<<32 | uint64(sys.Ino)
}
