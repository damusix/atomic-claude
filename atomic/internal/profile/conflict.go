// ~/.atomic/profile.md is the authoritative user profile. A divergent native copy
// is a conflict, never an import source and never overwritten in place.
package profile

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// NativeConflict names a harness-native profile copy compared against the
// authority.
type NativeConflict struct {
	Authority string
	Native    string
}

// LegacyProfilePath returns the pre-relocation profile path Claude-only installs
// wrote, usually a compat symlink to the authority.
func LegacyProfilePath(home string) string {
	return filepath.Join(home, ".claude", ".atomic", "profile.md")
}

// DetectNativeConflict reports whether the native copy at nativePath differs from
// ~/.atomic/profile.md. Only differing bytes are a conflict; it writes nothing.
func DetectNativeConflict(home, nativePath string) (NativeConflict, bool, error) {
	authority := config.ProfilePath(home)
	conflict := NativeConflict{Authority: authority, Native: nativePath}

	authorityInfo, err := os.Stat(authority)
	if err != nil {
		if os.IsNotExist(err) {
			return conflict, false, nil
		}
		return conflict, false, fmt.Errorf("profile conflict: stat %s: %w", authority, err)
	}

	nativeInfo, err := os.Stat(nativePath)
	if err != nil {
		if os.IsNotExist(err) {
			return conflict, false, nil
		}
		return conflict, false, fmt.Errorf("profile conflict: stat %s: %w", nativePath, err)
	}
	if os.SameFile(authorityInfo, nativeInfo) {
		return conflict, false, nil
	}

	authorityBytes, err := os.ReadFile(authority)
	if err != nil {
		return conflict, false, fmt.Errorf("profile conflict: read %s: %w", authority, err)
	}
	nativeBytes, err := os.ReadFile(nativePath)
	if err != nil {
		return conflict, false, fmt.Errorf("profile conflict: read %s: %w", nativePath, err)
	}
	return conflict, !bytes.Equal(authorityBytes, nativeBytes), nil
}
