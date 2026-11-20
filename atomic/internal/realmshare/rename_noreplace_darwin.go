package realmshare

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

func renameShareNoReplace(fromDir *os.File, fromName string, toDir *os.File, toName string) error {
	err := unix.RenameatxNp(int(fromDir.Fd()), fromName, int(toDir.Fd()), toName, unix.RENAME_EXCL)
	runtime.KeepAlive(fromDir)
	runtime.KeepAlive(toDir)
	return err
}

func shareEntryIsMount(parent *os.File, name string) (bool, error) {
	parentFD := int(parent.Fd())
	var parentFS, entryFS unix.Statfs_t
	if err := unix.Fstatfs(parentFD, &parentFS); err != nil {
		return false, err
	}
	entryFD, err := unix.Openat(parentFD, name, unix.O_EVTONLY|unix.O_SYMLINK|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	runtime.KeepAlive(parent)
	if err != nil {
		return false, err
	}
	defer unix.Close(entryFD)
	if err := unix.Fstatfs(entryFD, &entryFS); err != nil {
		return false, err
	}
	if parentFS.Fsid == (unix.Fsid{}) || entryFS.Fsid == (unix.Fsid{}) {
		return false, errors.New("filesystem ID unavailable")
	}
	return parentFS.Fsid != entryFS.Fsid, nil
}

func restorationIdentity(file *os.File) (string, error) {
	var stat unix.Stat_t
	err := unix.Fstat(int(file.Fd()), &stat)
	runtime.KeepAlive(file)
	if err != nil {
		return "", err
	}
	if stat.Ino == 0 {
		return "", errors.New("inode identity unavailable")
	}
	return fmt.Sprintf("darwin:%x:%x:%x:%x:%x", stat.Dev, stat.Ino, stat.Gen, stat.Btim.Sec, stat.Btim.Nsec), nil
}
