//go:build windows

package doctor

import (
	"os"
)

// inodeKey on Windows falls back to a weak fingerprint (size + modtime).
// Windows doesn't expose inode numbers via the standard library without cgo.
func inodeKey(info os.FileInfo) uint64 {
	return uint64(info.Size()) ^ uint64(info.ModTime().UnixNano())
}
