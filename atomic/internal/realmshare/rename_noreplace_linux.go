package realmshare

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

func renameShareNoReplace(fromDir *os.File, fromName string, toDir *os.File, toName string) error {
	err := unix.Renameat2(int(fromDir.Fd()), fromName, int(toDir.Fd()), toName, unix.RENAME_NOREPLACE)
	runtime.KeepAlive(fromDir)
	runtime.KeepAlive(toDir)
	return err
}

func shareEntryIsMount(parent *os.File, name string) (bool, error) {
	parentFD := int(parent.Fd())
	var parentStat, entryStat unix.Statx_t
	if err := unix.Statx(parentFD, "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &parentStat); err != nil {
		return false, err
	}
	err := unix.Statx(parentFD, name, unix.AT_SYMLINK_NOFOLLOW|unix.AT_NO_AUTOMOUNT, unix.STATX_MNT_ID, &entryStat)
	runtime.KeepAlive(parent)
	if err != nil {
		return false, err
	}
	if parentStat.Mask&entryStat.Mask&unix.STATX_MNT_ID == 0 {
		return false, errors.New("statx mount ID unavailable")
	}
	return parentStat.Mnt_id != entryStat.Mnt_id, nil
}

func restorationIdentity(file *os.File) (string, error) {
	var stat unix.Statx_t
	err := unix.Statx(int(file.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_INO|unix.STATX_BTIME, &stat)
	runtime.KeepAlive(file)
	if err != nil {
		return "", err
	}
	if stat.Mask&unix.STATX_INO == 0 || stat.Ino == 0 {
		return "", errors.New("restoration requires a filesystem that reports statx inode identity")
	}
	if stat.Mask&unix.STATX_BTIME == 0 {
		return "", errors.New("restoration requires a filesystem that reports statx birth time")
	}
	return fmt.Sprintf("linux:%x:%x:%x:%x:%x", stat.Dev_major, stat.Dev_minor, stat.Ino, stat.Btime.Sec, stat.Btime.Nsec), nil
}
