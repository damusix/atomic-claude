package profile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/config"
)

func writeProfile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A divergent native copy after adoption is a conflict; identical bytes are not.
func TestDetectNativeConflict(t *testing.T) {
	home := t.TempDir()
	native := filepath.Join(home, ".claude", "profile.md")
	writeProfile(t, config.ProfilePath(home), "# profile\nname: Danilo\n")
	writeProfile(t, native, "# profile\nname: Danilo\n")

	if _, conflict, err := DetectNativeConflict(home, native); err != nil || conflict {
		t.Fatalf("identical copies: conflict=%v err=%v", conflict, err)
	}

	writeProfile(t, native, "# profile\nname: Someone Else\n")
	c, conflict, err := DetectNativeConflict(home, native)
	if err != nil {
		t.Fatalf("DetectNativeConflict: %v", err)
	}
	if !conflict {
		t.Fatal("divergent native copy must be a conflict")
	}
	if c.Authority != config.ProfilePath(home) || c.Native != native {
		t.Errorf("conflict paths = %+v", c)
	}

	// Detection never imports: the authority bytes are untouched.
	data, err := os.ReadFile(config.ProfilePath(home))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# profile\nname: Danilo\n" {
		t.Errorf("authority was rewritten by detection: %q", data)
	}
}

// An absent authority or an absent native copy is not a conflict.
func TestDetectNativeConflict_AbsentIsNotConflict(t *testing.T) {
	home := t.TempDir()
	native := filepath.Join(home, ".claude", "profile.md")

	if _, conflict, err := DetectNativeConflict(home, native); err != nil || conflict {
		t.Fatalf("absent authority: conflict=%v err=%v", conflict, err)
	}

	writeProfile(t, config.ProfilePath(home), "# profile\n")
	if _, conflict, err := DetectNativeConflict(home, native); err != nil || conflict {
		t.Fatalf("absent native copy: conflict=%v err=%v", conflict, err)
	}
}

// The pre-relocation path is usually the same file through the compat symlink;
// two names for one file are never a conflict.
func TestDetectNativeConflict_SameFileThroughSymlink(t *testing.T) {
	home := t.TempDir()
	writeProfile(t, config.ProfilePath(home), "# profile\n")
	claudeAtomic := filepath.Join(home, ".claude", ".atomic")
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(config.Dir(home), claudeAtomic); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	if _, conflict, err := DetectNativeConflict(home, LegacyProfilePath(home)); err != nil || conflict {
		t.Fatalf("symlinked native copy: conflict=%v err=%v", conflict, err)
	}
}
