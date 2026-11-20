package realmshare

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

func renameShareNoReplace(fromDir *os.File, fromName string, toDir *os.File, toName string) error {
	from, err := windows.UTF16PtrFromString(filepath.Join(fromDir.Name(), fromName))
	if err != nil {
		return err
	}
	to, err := windows.UTF16PtrFromString(filepath.Join(toDir.Name(), toName))
	if err != nil {
		return err
	}
	return windows.MoveFileEx(from, to, 0)
}

func shareEntryIsMount(*os.File, string) (bool, error) {
	return false, fmt.Errorf("mount point detection on Windows: %w", errors.ErrUnsupported)
}

func restorationIdentity(file *os.File) (string, error) {
	var info struct {
		volumeSerialNumber uint64
		fileID             [16]byte
	}
	err := windows.GetFileInformationByHandleEx(windows.Handle(file.Fd()), windows.FileIdInfo, (*byte)(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	runtime.KeepAlive(file)
	if err != nil {
		return "", err
	}
	if info.fileID == ([16]byte{}) {
		return "", errors.New("file ID unavailable")
	}
	return fmt.Sprintf("windows:%x:%x", info.volumeSerialNumber, info.fileID), nil
}
