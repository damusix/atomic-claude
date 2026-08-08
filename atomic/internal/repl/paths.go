package repl

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// scopeKeyLen is how many hex characters of the scope-root digest name a
// session directory. Twelve is short enough to keep the socket path inside
// sun_path (see maxSocketPathLen) and wide enough that two roots on one machine
// colliding is not a practical concern.
const scopeKeyLen = 12

// maxSocketPathLen bounds a unix socket path. The kernel copies it into
// sockaddr_un.sun_path, which is 104 bytes on macOS and 108 on Linux, NUL
// included — so 103 is the length that binds on both. Applying the stricter
// figure everywhere costs nothing (these paths are short by construction) and
// spares a session from working on one platform and failing on the other.
const maxSocketPathLen = 103

// ScopeKey derives a session directory name from the scope root a session keys
// to (a repo root, or a realm root when `start` ran at one). Hashing rather
// than embedding the path is what bounds the socket path: an arbitrarily deep
// repo still produces a fixed-width directory name.
//
// The root is cleaned first, so the same directory reached by a different
// spelling — a trailing slash, an interior "..", — resolves to the same
// sessions.
func ScopeKey(scopeRoot string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(scopeRoot)))
	return hex.EncodeToString(sum[:])[:scopeKeyLen]
}

// RootDir returns <home>/.atomic/repl, where every scope's sessions live.
//
// home is a parameter and never os.UserHomeDir: the whole package is
// exercisable against a temp home, and a test that reaches the real one can
// delete a live session.
func RootDir(home string) string {
	return filepath.Join(config.Dir(home), "repl")
}

// SessionDir returns the directory holding scopeRoot's sessions.
func SessionDir(home, scopeRoot string) string {
	return filepath.Join(RootDir(home), ScopeKey(scopeRoot))
}

// SocketPath returns the unix socket for one session, refusing a name that is
// not a path component and a path the kernel could not bind. Returning the
// error rather than the bare path is deliberate: every caller computes the path
// through here, so neither failure can slip past to a spawn.
func SocketPath(home, scopeRoot, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	path := filepath.Join(SessionDir(home, scopeRoot), name+".sock")
	if len(path) > maxSocketPathLen {
		return "", fmt.Errorf(
			"repl: socket path %s is %d bytes, over the %d-byte unix socket path limit; use a shorter session name",
			path, len(path), maxSocketPathLen)
	}
	return path, nil
}

// MetaPath returns the session's meta file. It is an ordinary file, so only the
// name is validated — the socket length limit does not apply.
func MetaPath(home, scopeRoot, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	return filepath.Join(SessionDir(home, scopeRoot), name+".meta.json"), nil
}

// LockPath returns the flock file guarding one session's probe-and-spawn
// sequence, so two concurrent `start` calls on one name produce one harness.
func LockPath(home, scopeRoot, name string) (string, error) {
	if err := ValidateName(name); err != nil {
		return "", err
	}
	return filepath.Join(SessionDir(home, scopeRoot), name+".lock"), nil
}

// AllSessionDirs returns every scope directory under RootDir, for machine-wide
// enumeration (`list --all`). A root that does not exist yet means no session
// has ever been started — an empty result, not an error.
//
// Only directories whose name has the shape of a scope key are returned, so
// unrelated debris under the root is never presented as a scope.
func AllSessionDirs(home string) ([]string, error) {
	root := RootDir(home)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("repl: read %s: %w", root, err)
	}

	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() && isScopeKey(entry.Name()) {
			dirs = append(dirs, filepath.Join(root, entry.Name()))
		}
	}
	return dirs, nil
}

// ValidateName rejects a session name that is not usable as a single path
// component, before any file is touched. Mirrors the same check on bucket names
// in internal/wiki/bucket.go, for the same reason: the name becomes a filename.
func ValidateName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("repl: session name must not be empty")
	}
	if strings.HasPrefix(name, "-") {
		// Otherwise the name is indistinguishable from a flag on any command
		// line that carries it.
		return fmt.Errorf("repl: session name %q must not begin with %q", name, "-")
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("repl: session name %q must not contain a path separator", name)
	}
	if strings.ContainsRune(name, 0) {
		return fmt.Errorf("repl: session name %q must not contain a null byte", name)
	}
	if name == "." || name == ".." {
		return fmt.Errorf("repl: session name %q is not a valid path segment", name)
	}
	return nil
}

func isScopeKey(name string) bool {
	if len(name) != scopeKeyLen {
		return false
	}
	return strings.Trim(name, "0123456789abcdef") == ""
}
