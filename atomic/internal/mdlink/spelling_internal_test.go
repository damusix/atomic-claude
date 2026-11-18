package mdlink

import (
	"path/filepath"
	"testing"
)

func TestExactSpellingChecksEveryPathComponent(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, "deployment/manifest.yaml")
	entries := make(directoryEntries)
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"deployment/manifest.yaml", true},
		{"Deployment/manifest.yaml", false},
		{"deployment/Manifest.yaml", false},
	} {
		if got := entries.hasExactSpelling(dir, filepath.Join(dir, tc.path)); got != tc.want {
			t.Errorf("exact spelling of %q: got %t, want %t", tc.path, got, tc.want)
		}
	}
}
