package installstate

import (
	"os"
	"syscall"
)

// flockExclusive blocks until this descriptor holds an exclusive advisory lock.
func flockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// flockUnlock releases an advisory lock held by this descriptor.
func flockUnlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
