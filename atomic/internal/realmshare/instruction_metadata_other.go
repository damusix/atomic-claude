//go:build !darwin && !linux

package realmshare

import (
	"errors"
	"os"
)

func instructionOwnership(os.FileInfo, bool) string {
	return ""
}

func instructionACL(*os.File, bool) error {
	return errors.New("ACL metadata is unavailable on this platform")
}
