//go:build windows

package doctor

import (
	"os"
)

// inodeKey uses a weak size+modtime fingerprint: Windows exposes no inode
// number through the standard library without cgo.
func inodeKey(info os.FileInfo) uint64 {
	return uint64(info.Size()) ^ uint64(info.ModTime().UnixNano())
}
