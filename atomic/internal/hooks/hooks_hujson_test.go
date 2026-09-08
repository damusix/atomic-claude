package hooks

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/tailscale/hujson"
)

// TestWriteSettingsHujson_SymlinkedTarget_StaysSymlink guards the dotfiles-repo
// case: a plain os.Rename onto a symlink would replace it with a regular file
// and sever the link, so the write must resolve through it instead.
func TestWriteSettingsHujson_SymlinkedTarget_StaysSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real-settings.json")
	if err := os.WriteFile(real, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "settings.json")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	ast, err := hujson.Parse([]byte(`{"theme":"dark"}`))
	if err != nil {
		t.Fatal(err)
	}
	skipped, err := writeSettingsHujson(link, ast)
	if err != nil {
		t.Fatalf("writeSettingsHujson: %v", err)
	}
	if skipped {
		t.Fatal("expected write, got skipped")
	}

	fi, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("Lstat: %v", err)
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("settings.json is no longer a symlink after write")
	}

	dest, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if dest != real {
		t.Fatalf("symlink destination changed: got %q, want %q", dest, real)
	}

	content, err := os.ReadFile(real)
	if err != nil {
		t.Fatalf("ReadFile real target: %v", err)
	}
	if !strings.Contains(string(content), `"theme":"dark"`) {
		t.Fatalf("real target does not hold new content: %q", content)
	}
}

// TestWriteSettingsHujson_ReadOnlyTarget_SkippedNotModified guards the user's
// deliberate chmod: os.Rename needs only directory write permission, so
// without this probe it would bypass the read-only file.
func TestWriteSettingsHujson_ReadOnlyTarget_SkippedNotModified(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only permission semantics differ on windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permission checks")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	original := []byte(`{"theme":"dark"}` + "\n")
	if err := os.WriteFile(target, original, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(target, 0o644) })

	ast, err := hujson.Parse([]byte(`{"theme":"light"}`))
	if err != nil {
		t.Fatal(err)
	}
	skipped, err := writeSettingsHujson(target, ast)
	if err != nil {
		t.Fatalf("writeSettingsHujson: %v", err)
	}
	if !skipped {
		t.Fatal("expected skipped=true for read-only target")
	}

	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(original) {
		t.Fatalf("read-only target was modified: got %q, want %q", content, original)
	}
}

// TestWriteSettingsHujson_ModePreserved guards a user's non-default chmod
// (e.g. 0600) surviving the temp-file + rename round trip.
func TestWriteSettingsHujson_ModePreserved(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(target, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ast, err := hujson.Parse([]byte(`{"theme":"dark"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writeSettingsHujson(target, ast); err != nil {
		t.Fatalf("writeSettingsHujson: %v", err)
	}

	fi, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

// TestWriteSettingsHujson_ContentMatchesPriorImplementation guards the
// newline and hujson-packing behavior the old truncate-in-place write
// produced, on both a missing and an existing target.
func TestWriteSettingsHujson_ContentMatchesPriorImplementation(t *testing.T) {
	ast, err := hujson.Parse([]byte(`{
  // a comment
  "theme": "dark",
}
`))
	if err != nil {
		t.Fatal(err)
	}
	want := ast.Pack()
	if len(want) > 0 && want[len(want)-1] != '\n' {
		want = append(want, '\n')
	}

	t.Run("missing target", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "nested", "settings.json")
		if _, err := writeSettingsHujson(target, ast); err != nil {
			t.Fatalf("writeSettingsHujson: %v", err)
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("content mismatch:\ngot:  %q\nwant: %q", got, want)
		}
	})

	t.Run("existing target", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(target, []byte(`{"old":true}`), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := writeSettingsHujson(target, ast); err != nil {
			t.Fatalf("writeSettingsHujson: %v", err)
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("content mismatch:\ngot:  %q\nwant: %q", got, want)
		}
	})

	// The fixture above already ends in a newline, so it never reaches the
	// append branch. A source with no trailing newline is what proves the
	// written file still gets one.
	t.Run("source without trailing newline", func(t *testing.T) {
		bare, err := hujson.Parse([]byte(`{"theme":"dark"}`))
		if err != nil {
			t.Fatal(err)
		}
		if packed := bare.Pack(); packed[len(packed)-1] == '\n' {
			t.Fatalf("fixture no longer exercises the append branch: %q", packed)
		}

		dir := t.TempDir()
		target := filepath.Join(dir, "settings.json")
		if _, err := writeSettingsHujson(target, bare); err != nil {
			t.Fatalf("writeSettingsHujson: %v", err)
		}
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 0 || got[len(got)-1] != '\n' {
			t.Errorf("want trailing newline appended, got %q", got)
		}
		if string(got) != `{"theme":"dark"}`+"\n" {
			t.Errorf("content mismatch: got %q", got)
		}
	})
}
