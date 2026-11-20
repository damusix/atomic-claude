package realmshare

import (
	"fmt"
	"os"
	"runtime"
	"syscall"

	"golang.org/x/sys/unix"
)

func instructionOwnership(info os.FileInfo, creating bool) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	uid, gid := stat.Uid, stat.Gid
	if creating {
		uid = uint32(os.Geteuid())
		if info.Mode()&os.ModeSetgid == 0 {
			gid = uint32(os.Getegid())
		}
	}
	return fmt.Sprintf("%08x%08x", uid, gid)
}

func instructionACL(file *os.File, directory bool) error {
	attr := "system.posix_acl_access"
	if directory {
		attr = "system.posix_acl_default"
	}
	_, err := unix.Fgetxattr(int(file.Fd()), attr, nil)
	runtime.KeepAlive(file)
	if err == unix.ENODATA {
		return nil
	}
	if err == nil {
		return fmt.Errorf("an ACL is present (%s)", attr)
	}
	return fmt.Errorf("cannot establish ACL absence: %w", err)
}
