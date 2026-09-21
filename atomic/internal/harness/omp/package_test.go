package omp

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// Render must pin every node's permission bits, or the published tree's digest
// depends on the caller's umask and a converge from a different shell reads a
// freshly published tree as stale.
func TestRenderIsUmaskIndependent(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)

	dir := t.TempDir()
	pkg := Package{Files: []PackageFile{
		{Path: "commands/a.md", Bytes: []byte("a")},
		{Path: "skills/b/SKILL.md", Bytes: []byte("b")},
	}}
	if err := pkg.Render(dir); err != nil {
		t.Fatalf("Render: %v", err)
	}
	for name, want := range map[string]os.FileMode{
		"commands":          0o755,
		"commands/a.md":     0o644,
		"skills":            0o755,
		"skills/b":          0o755,
		"skills/b/SKILL.md": 0o644,
	} {
		info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %04o, want %04o", name, got, want)
		}
	}

	// The digest is therefore stable under either umask.
	digest, err := pkg.TreeDigest()
	if err != nil {
		t.Fatal(err)
	}
	syscall.Umask(old)
	other, err := pkg.TreeDigest()
	if err != nil {
		t.Fatal(err)
	}
	if digest != other {
		t.Errorf("tree digest %s != %s across umasks", digest, other)
	}
}
