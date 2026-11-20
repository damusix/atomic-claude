//go:build !darwin && !linux && !windows

package realmshare

import (
	"errors"
	"fmt"
	"os"
)

func renameShareNoReplace(*os.File, string, *os.File, string) error {
	return errors.ErrUnsupported
}

func shareEntryIsMount(*os.File, string) (bool, error) {
	return false, fmt.Errorf("mount point detection: %w", errors.ErrUnsupported)
}

func restorationIdentity(*os.File) (string, error) {
	return "", fmt.Errorf("restoration identity: %w", errors.ErrUnsupported)
}
