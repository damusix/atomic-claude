package repl

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScopeKey_StableForARootAndDistinctAcrossRoots(t *testing.T) {
	const root = "/Users/someone/projects/thing"

	first := ScopeKey(root)
	if second := ScopeKey(root); first != second {
		t.Errorf("ScopeKey(%q) = %q then %q — a session's directory must not move between calls", root, first, second)
	}
	// Trailing slashes and interior no-ops are the same root: `atomic repl
	// eval` run from a path spelled differently must find the session `start`
	// created.
	for _, spelling := range []string{root + "/", root + "/.", "/Users/someone/projects/other/../thing"} {
		if got := ScopeKey(spelling); got != first {
			t.Errorf("ScopeKey(%q) = %q, want %q — the same root spelled differently keys to the same session dir", spelling, got, first)
		}
	}
	if other := ScopeKey("/Users/someone/projects/sibling"); other == first {
		t.Errorf("ScopeKey collided across distinct roots: both %q", other)
	}

	if len(first) != scopeKeyLen {
		t.Errorf("ScopeKey length = %d, want %d — the fixed length is what keeps the socket path inside sun_path", len(first), scopeKeyLen)
	}
	if strings.Trim(first, "0123456789abcdef") != "" {
		t.Errorf("ScopeKey = %q, want lowercase hex only — AllSessionDirs identifies session dirs by that shape", first)
	}
}

func TestPaths_ResolveAgainstTheInjectedHome(t *testing.T) {
	// shortTempDir, not t.TempDir: on macOS the per-test $TMPDIR path alone is
	// long enough to push a socket beneath it past sun_path.
	home := shortTempDir(t)
	const root = "/repo"

	if got, want := RootDir(home), filepath.Join(home, ".atomic", "repl"); got != want {
		t.Errorf("RootDir = %q, want %q", got, want)
	}
	if got, want := SessionDir(home, root), filepath.Join(RootDir(home), ScopeKey(root)); got != want {
		t.Errorf("SessionDir = %q, want %q", got, want)
	}

	sock, err := SocketPath(home, root, "work")
	if err != nil {
		t.Fatalf("SocketPath: %v", err)
	}
	meta, err := MetaPath(home, root, "work")
	if err != nil {
		t.Fatalf("MetaPath: %v", err)
	}
	for _, path := range []string{sock, meta} {
		// Never os.UserHomeDir internally: a test that leaks into the real
		// home is a test that can delete a live session.
		if !strings.HasPrefix(path, home) {
			t.Errorf("%q does not resolve under the injected home %q", path, home)
		}
	}
	if filepath.Base(sock) != "work.sock" {
		t.Errorf("socket base = %q, want %q", filepath.Base(sock), "work.sock")
	}
	if filepath.Base(meta) != "work.meta.json" {
		t.Errorf("meta base = %q, want %q", filepath.Base(meta), "work.meta.json")
	}
}

func TestValidateName_RejectsAnythingThatIsNotAPathComponent(t *testing.T) {
	tests := []struct {
		name string
		arg  string
		ok   bool
	}{
		{"ordinary", "work", true},
		{"dashes and digits inside", "my-session-2", true},
		{"underscore", "my_session", true},
		{"empty", "", false},
		{"whitespace only", "   ", false},
		{"leading dash reads as a flag", "-name", false},
		{"forward slash", "a/b", false},
		{"backslash", "a\\b", false},
		{"dot", ".", false},
		{"dot dot", "..", false},
		{"parent traversal", "../escape", false},
		{"null byte", "a\x00b", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateName(tc.arg)
			if tc.ok && err != nil {
				t.Fatalf("ValidateName(%q) = %v, want nil", tc.arg, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("ValidateName(%q) = nil, want a rejection", tc.arg)
			}
		})
	}
}

func TestSocketAndMetaPath_RefuseAnInvalidName(t *testing.T) {
	home := t.TempDir()
	// The path is never computed from an unvalidated name, so a traversal
	// attempt cannot reach the filesystem at all.
	if got, err := SocketPath(home, "/repo", "../escape"); err == nil {
		t.Errorf("SocketPath returned %q for a traversing name, want an error", got)
	}
	if got, err := MetaPath(home, "/repo", "../escape"); err == nil {
		t.Errorf("MetaPath returned %q for a traversing name, want an error", got)
	}
}

func TestSocketPath_FailsLoudOverTheUnixSocketLimit(t *testing.T) {
	// A home deep enough to push the computed socket past sun_path. The point
	// is a named error before any spawn, not a silently truncated path that
	// binds somewhere else.
	home := "/" + strings.Repeat("deep/", 25)

	_, err := SocketPath(home, "/repo", "work")
	if err == nil {
		t.Fatal("SocketPath accepted a path over the unix-socket limit")
	}
	for _, want := range []string{"socket path", "limit"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	// Meta is an ordinary file — the limit is a socket constraint only.
	if _, err := MetaPath(home, "/repo", "work"); err != nil {
		t.Errorf("MetaPath = %v, want nil — only the socket is bound by sun_path", err)
	}
}

func TestAllSessionDirs(t *testing.T) {
	home := t.TempDir()

	// Absent root: no sessions have ever been started, which is not an error.
	dirs, err := AllSessionDirs(home)
	if err != nil {
		t.Fatalf("AllSessionDirs on an absent root: %v", err)
	}
	if len(dirs) != 0 {
		t.Errorf("AllSessionDirs = %v, want empty", dirs)
	}

	wantA := SessionDir(home, "/repo/a")
	wantB := SessionDir(home, "/repo/b")
	for _, dir := range []string{wantA, wantB} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	// Debris that is not a scope key: a stray file and a directory named
	// something else must not be offered up as a scope.
	if err := os.WriteFile(filepath.Join(RootDir(home), "stray.txt"), []byte("x"), 0o600); err != nil {
		t.Fatalf("write stray file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(RootDir(home), "not-a-scope-key"), 0o700); err != nil {
		t.Fatalf("mkdir stray dir: %v", err)
	}

	dirs, err = AllSessionDirs(home)
	if err != nil {
		t.Fatalf("AllSessionDirs: %v", err)
	}
	got := map[string]bool{}
	for _, dir := range dirs {
		got[dir] = true
	}
	if len(got) != 2 || !got[wantA] || !got[wantB] {
		t.Errorf("AllSessionDirs = %v, want exactly %v and %v", dirs, wantA, wantB)
	}
}
