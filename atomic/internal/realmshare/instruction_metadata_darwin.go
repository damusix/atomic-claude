package realmshare

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func instructionOwnership(info os.FileInfo, creating bool) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	uid := stat.Uid
	if creating {
		uid = uint32(os.Geteuid())
	}
	return fmt.Sprintf("%08x%08x", uid, stat.Gid)
}

func instructionACL(file *os.File, _ bool) error {
	const (
		attrCmnExtendedSecurity = 0x00400000
		fsoptReportFullSize     = 4
	)
	attrs := unix.Attrlist{Bitmapcount: 5, Commonattr: attrCmnExtendedSecurity}
	// The security xattr is protected. Read the length and attrreference from fgetattrlist instead.
	var reply struct {
		total   uint32
		offset  int32
		aclSize uint32
	}
	_, _, errno := syscall.Syscall6(unix.SYS_FGETATTRLIST, file.Fd(), uintptr(unsafe.Pointer(&attrs)),
		uintptr(unsafe.Pointer(&reply)), unsafe.Sizeof(reply), fsoptReportFullSize, 0)
	runtime.KeepAlive(file)
	if errno != 0 {
		return errno
	}
	if reply.aclSize != 0 {
		return errors.New("an extended ACL is present")
	}
	if reply.total != uint32(unsafe.Sizeof(reply)) || reply.offset != 8 {
		return errors.New("invalid extended security attribute reply")
	}
	return nil
}
