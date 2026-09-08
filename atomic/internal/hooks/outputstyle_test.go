package hooks

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeStyleFile(t *testing.T, targetDir string) {
	t.Helper()
	dir := filepath.Join(targetDir, "output-styles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir output-styles: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "atomic.md"), []byte("---\nname: Atomic\n---\n"), 0o644); err != nil {
		t.Fatalf("write atomic.md: %v", err)
	}
}

func TestSeedOutputStyle_SeedsIntoMissingFile(t *testing.T) {
	scopeRoot := t.TempDir()
	targetDir := t.TempDir()
	home := t.TempDir()
	writeStyleFile(t, targetDir)

	wrote, err := SeedOutputStyle(scopeRoot, targetDir, home)
	if err != nil {
		t.Fatalf("SeedOutputStyle: %v", err)
	}
	if !wrote {
		t.Fatal("expected wrote == true")
	}

	value, present, err := ReadOutputStyle(SettingsPath(scopeRoot))
	if err != nil {
		t.Fatalf("ReadOutputStyle: %v", err)
	}
	if !present || value != OutputStyleName {
		t.Fatalf("got present=%v value=%q, want present=true value=%q", present, value, OutputStyleName)
	}
}

func TestSeedOutputStyle_PreservesOtherKeysAndComments(t *testing.T) {
	scopeRoot := t.TempDir()
	targetDir := t.TempDir()
	home := t.TempDir()
	writeStyleFile(t, targetDir)

	sfPath := SettingsPath(scopeRoot)
	if err := os.MkdirAll(filepath.Dir(sfPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	initial := "{\n  // keep me\n  \"env\": {\"FOO\": \"bar\"},\n}\n"
	if err := os.WriteFile(sfPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial settings: %v", err)
	}

	wrote, err := SeedOutputStyle(scopeRoot, targetDir, home)
	if err != nil {
		t.Fatalf("SeedOutputStyle: %v", err)
	}
	if !wrote {
		t.Fatal("expected wrote == true")
	}

	out, err := os.ReadFile(sfPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	got := string(out)
	for _, want := range []string{"// keep me", `"FOO": "bar"`, `"outputStyle":"Atomic"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("settings.json missing %q, got:\n%s", want, got)
		}
	}
}

func TestSeedOutputStyle_NeverOverwritesExistingValue(t *testing.T) {
	scopeRoot := t.TempDir()
	targetDir := t.TempDir()
	home := t.TempDir()
	writeStyleFile(t, targetDir)

	sfPath := SettingsPath(scopeRoot)
	if err := os.MkdirAll(filepath.Dir(sfPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	initial := "{\n  \"outputStyle\": \"Explanatory\"\n}\n"
	if err := os.WriteFile(sfPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("write initial settings: %v", err)
	}

	wrote, err := SeedOutputStyle(scopeRoot, targetDir, home)
	if err != nil {
		t.Fatalf("SeedOutputStyle: %v", err)
	}
	if wrote {
		t.Fatal("expected wrote == false when outputStyle already present")
	}

	out, err := os.ReadFile(sfPath)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if string(out) != initial {
		t.Fatalf("settings.json was rewritten, got:\n%s\nwant unchanged:\n%s", out, initial)
	}
}

func TestSeedOutputStyle_SkipsWhenStyleFileAbsent(t *testing.T) {
	scopeRoot := t.TempDir()
	targetDir := t.TempDir()
	home := t.TempDir()
	// No output-styles/atomic.md written.

	wrote, err := SeedOutputStyle(scopeRoot, targetDir, home)
	if err != nil {
		t.Fatalf("SeedOutputStyle: %v", err)
	}
	if wrote {
		t.Fatal("expected wrote == false when style file absent")
	}
	if _, err := os.Stat(SettingsPath(scopeRoot)); !os.IsNotExist(err) {
		t.Fatalf("expected settings.json to remain absent, stat err = %v", err)
	}
}

func TestSeedOutputStyle_SkipsWhenSeedDisabled(t *testing.T) {
	scopeRoot := t.TempDir()
	targetDir := t.TempDir()
	home := t.TempDir()
	writeStyleFile(t, targetDir)

	tomlPath := filepath.Join(home, ".atomic", "config.toml")
	if err := os.MkdirAll(filepath.Dir(tomlPath), 0o755); err != nil {
		t.Fatalf("mkdir .atomic: %v", err)
	}
	if err := os.WriteFile(tomlPath, []byte("[output_style]\nseed = false\n"), 0o644); err != nil {
		t.Fatalf("write config.toml: %v", err)
	}

	wrote, err := SeedOutputStyle(scopeRoot, targetDir, home)
	if err != nil {
		t.Fatalf("SeedOutputStyle: %v", err)
	}
	if wrote {
		t.Fatal("expected wrote == false when output_style.seed is false")
	}
}

func TestSeedOutputStyle_SkipsOnReadOnlySettingsFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("read-only permission semantics differ on windows")
	}

	scopeRoot := t.TempDir()
	targetDir := t.TempDir()
	home := t.TempDir()
	writeStyleFile(t, targetDir)

	sfPath := SettingsPath(scopeRoot)
	if err := os.MkdirAll(filepath.Dir(sfPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(sfPath, []byte("{}\n"), 0o444); err != nil {
		t.Fatalf("write read-only settings: %v", err)
	}
	t.Cleanup(func() { os.Chmod(sfPath, 0o644) })

	wrote, err := SeedOutputStyle(scopeRoot, targetDir, home)
	if err != nil {
		t.Fatalf("SeedOutputStyle: %v", err)
	}
	if wrote {
		t.Fatal("expected wrote == false for a read-only settings file")
	}
}

func TestSeedOutputStyle_MalformedSettingsFileReturnsError(t *testing.T) {
	scopeRoot := t.TempDir()
	targetDir := t.TempDir()
	home := t.TempDir()
	writeStyleFile(t, targetDir)

	sfPath := SettingsPath(scopeRoot)
	if err := os.MkdirAll(filepath.Dir(sfPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(sfPath, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("write malformed settings: %v", err)
	}

	if _, err := SeedOutputStyle(scopeRoot, targetDir, home); err == nil {
		t.Fatal("expected an error for malformed settings.json")
	}
}

func TestReadOutputStyle(t *testing.T) {
	t.Run("missing file", func(t *testing.T) {
		value, present, err := ReadOutputStyle(filepath.Join(t.TempDir(), ".claude", "settings.json"))
		if err != nil {
			t.Fatalf("ReadOutputStyle: %v", err)
		}
		if present || value != "" {
			t.Fatalf("got present=%v value=%q, want present=false value=\"\"", present, value)
		}
	})

	t.Run("absent key", func(t *testing.T) {
		dir := t.TempDir()
		sfPath := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(sfPath, []byte(`{"env": {"FOO": "bar"}}`), 0o644); err != nil {
			t.Fatalf("write settings: %v", err)
		}
		value, present, err := ReadOutputStyle(sfPath)
		if err != nil {
			t.Fatalf("ReadOutputStyle: %v", err)
		}
		if present || value != "" {
			t.Fatalf("got present=%v value=%q, want present=false value=\"\"", present, value)
		}
	})

	t.Run("present key", func(t *testing.T) {
		dir := t.TempDir()
		sfPath := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(sfPath, []byte(`{"outputStyle": "Explanatory"}`), 0o644); err != nil {
			t.Fatalf("write settings: %v", err)
		}
		value, present, err := ReadOutputStyle(sfPath)
		if err != nil {
			t.Fatalf("ReadOutputStyle: %v", err)
		}
		if !present || value != "Explanatory" {
			t.Fatalf("got present=%v value=%q, want present=true value=%q", present, value, "Explanatory")
		}
	})

	// A non-string value can only arrive from another tool editing the file.
	// Reporting it as present with a rendered value beats an empty string a
	// caller cannot tell apart from an explicitly empty one.
	t.Run("non-string value", func(t *testing.T) {
		dir := t.TempDir()
		sfPath := filepath.Join(dir, "settings.json")
		if err := os.WriteFile(sfPath, []byte(`{"outputStyle": 42}`), 0o644); err != nil {
			t.Fatalf("write settings: %v", err)
		}
		value, present, err := ReadOutputStyle(sfPath)
		if err != nil {
			t.Fatalf("ReadOutputStyle: %v", err)
		}
		if !present || value == "" {
			t.Fatalf("got present=%v value=%q, want present=true and a non-empty rendering", present, value)
		}
	})
}

func TestRemoveOutputStyleIfAtomic(t *testing.T) {
	t.Run("removes exactly Atomic", func(t *testing.T) {
		scopeRoot := t.TempDir()
		sfPath := SettingsPath(scopeRoot)
		if err := os.MkdirAll(filepath.Dir(sfPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(sfPath, []byte(`{"outputStyle": "Atomic"}`+"\n"), 0o644); err != nil {
			t.Fatalf("write settings: %v", err)
		}

		removed, _, err := RemoveOutputStyleIfAtomic(scopeRoot)
		if err != nil {
			t.Fatalf("RemoveOutputStyleIfAtomic: %v", err)
		}
		if !removed {
			t.Fatal("expected removed == true")
		}

		_, present, err := ReadOutputStyle(sfPath)
		if err != nil {
			t.Fatalf("ReadOutputStyle: %v", err)
		}
		if present {
			t.Fatal("expected outputStyle to be gone")
		}
	})

	t.Run("leaves Atomic untouched while the style file is still installed", func(t *testing.T) {
		scopeRoot := t.TempDir()
		writeStyleFile(t, filepath.Join(scopeRoot, ".claude"))
		sfPath := SettingsPath(scopeRoot)
		if err := os.WriteFile(sfPath, []byte(`{"outputStyle": "Atomic"}`+"\n"), 0o644); err != nil {
			t.Fatalf("write settings: %v", err)
		}

		removed, _, err := RemoveOutputStyleIfAtomic(scopeRoot)
		if err != nil {
			t.Fatalf("RemoveOutputStyleIfAtomic: %v", err)
		}
		if removed {
			t.Fatal("expected removed == false while the style file is still installed")
		}

		value, present, err := ReadOutputStyle(sfPath)
		if err != nil {
			t.Fatalf("ReadOutputStyle: %v", err)
		}
		if !present || value != OutputStyleName {
			t.Fatalf("got present=%v value=%q, want present=true value=%q", present, value, OutputStyleName)
		}
	})

	t.Run("leaves a different value untouched", func(t *testing.T) {
		scopeRoot := t.TempDir()
		sfPath := SettingsPath(scopeRoot)
		if err := os.MkdirAll(filepath.Dir(sfPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		initial := "{\n  // keep me\n  \"outputStyle\": \"Explanatory\"\n}\n"
		if err := os.WriteFile(sfPath, []byte(initial), 0o644); err != nil {
			t.Fatalf("write settings: %v", err)
		}

		removed, _, err := RemoveOutputStyleIfAtomic(scopeRoot)
		if err != nil {
			t.Fatalf("RemoveOutputStyleIfAtomic: %v", err)
		}
		if removed {
			t.Fatal("expected removed == false for a non-Atomic value")
		}

		out, err := os.ReadFile(sfPath)
		if err != nil {
			t.Fatalf("read settings: %v", err)
		}
		if string(out) != initial {
			t.Fatalf("settings.json was rewritten on a no-op, got:\n%s\nwant unchanged:\n%s", out, initial)
		}
	})

	t.Run("no-ops on absent key", func(t *testing.T) {
		scopeRoot := t.TempDir()
		sfPath := SettingsPath(scopeRoot)
		if err := os.MkdirAll(filepath.Dir(sfPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(sfPath, []byte(`{"env": {}}`), 0o644); err != nil {
			t.Fatalf("write settings: %v", err)
		}

		removed, _, err := RemoveOutputStyleIfAtomic(scopeRoot)
		if err != nil {
			t.Fatalf("RemoveOutputStyleIfAtomic: %v", err)
		}
		if removed {
			t.Fatal("expected removed == false when key absent")
		}
	})

	t.Run("no-ops on absent file", func(t *testing.T) {
		scopeRoot := t.TempDir()

		removed, _, err := RemoveOutputStyleIfAtomic(scopeRoot)
		if err != nil {
			t.Fatalf("RemoveOutputStyleIfAtomic: %v", err)
		}
		if removed {
			t.Fatal("expected removed == false when settings.json absent")
		}
	})
}
