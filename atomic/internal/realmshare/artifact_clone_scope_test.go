package realmshare

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArtifactNamespaceContainment(t *testing.T) {
	base := t.TempDir()
	existing := filepath.Join(base, "Profile")
	variant := filepath.Join(base, "profile")
	if err := os.Mkdir(existing, 0o700); err != nil {
		t.Fatal(err)
	}
	check := func(parent, child string, want bool) {
		t.Helper()
		p, err := readArtifactNamespace(parent)
		if err != nil {
			t.Fatal(err)
		}
		c, err := readArtifactNamespace(child)
		if err != nil {
			t.Fatal(err)
		}
		if got := p.contains(c, ""); got != want {
			t.Errorf("%q contains %q = %v, want %v", parent, child, got, want)
		}
	}
	check(filepath.Join(existing, "café"), filepath.Join(existing, "CAFE\u0301", "child"), true)
	check(filepath.Join(existing, "missing"), filepath.Join(existing, "missing-sibling", "child"), false)
	if err := os.Mkdir(variant, 0o700); err == nil {
		check(existing, filepath.Join(variant, "child"), false)
	} else if !os.IsExist(err) {
		t.Fatal(err)
	} else {
		check(existing, filepath.Join(variant, "child"), true)
	}
}
