package installstate

import (
	"errors"
	"os"
	"syscall"
)

// flockExclusive blocks until this descriptor holds an exclusive advisory lock.
func flockExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// flockTryExclusive takes the exclusive lock without waiting and reports
// whether another descriptor holds it.
func flockTryExclusive(f *os.File) (held bool, err error) {
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	return false, err
}

// flockUnlock releases an advisory lock held by this descriptor.
func flockUnlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
