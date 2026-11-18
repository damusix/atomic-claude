package managedfile

import (
	"os"
	"path/filepath"
	"testing"
)

// A symlink's owned content is its target text, so a link retargeted
// elsewhere no longer verifies even though it still resolves.
func TestObserveSymlinkDigestsItsTarget(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "atomic")
	if err := os.Mkdir(filepath.Join(dir, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("rules", link); err != nil {
		t.Fatal(err)
	}

	obs, err := Observe(link, KindSymlink)
	if err != nil {
		t.Fatalf("Observe: %v", err)
	}
	if obs.Kind != KindSymlink || obs.Digest != Digest([]byte("rules")) {
		t.Fatalf("observation = %+v, want a symlink digested by its target", obs)
	}
	if _, ok := obs.Verify(Digest([]byte("elsewhere"))); ok {
		t.Error("a different target verified")
	}

	missing, err := Observe(filepath.Join(dir, "absent"), KindSymlink)
	if err != nil || missing.Exists() {
		t.Errorf("missing link = %+v, %v; want absent", missing, err)
	}
	if _, err := Observe(filepath.Join(dir, "rules"), KindSymlink); err == nil {
		t.Error("a real directory observed as a symlink")
	}
}
