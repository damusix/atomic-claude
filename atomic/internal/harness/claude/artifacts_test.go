package claude

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// TestGlobalArtifactsMapSteeringToClaudeMd proves the adapter's projection
// surface: the global steering artifact lands directly on CLAUDE.md as a managed
// block, every other artifact is a whole file, and nothing projects to a
// user-level AGENTS.md.
func TestGlobalArtifactsMapSteeringToClaudeMd(t *testing.T) {
	root := "/home/u/.claude"
	artifacts, err := GlobalArtifacts(root)
	if err != nil {
		t.Fatalf("GlobalArtifacts: %v", err)
	}
	if len(artifacts) != len(embedded.Manifest()) {
		t.Fatalf("artifacts = %d, want %d", len(artifacts), len(embedded.Manifest()))
	}

	loader := GlobalLoaderPath(root)
	steering := 0
	for _, a := range artifacts {
		if a.Path == loader || filepath.Base(a.Path) == "AGENTS.md" {
			t.Errorf("artifact %s projects to a user-level AGENTS.md: %s", a.ID, a.Path)
		}
		if !strings.HasPrefix(a.Path, root+string(filepath.Separator)) {
			t.Errorf("artifact %s escapes the native root: %s", a.ID, a.Path)
		}
		if a.ID == steeringTarget {
			steering++
			if a.Kind != managedfile.KindBlock || a.Path != GlobalSteeringPath(root) {
				t.Errorf("steering artifact = %+v", a)
			}
		} else if a.Kind != managedfile.KindFile || len(a.Data) == 0 {
			t.Errorf("artifact %s = kind %s, %d bytes", a.ID, a.Kind, len(a.Data))
		}
	}
	if steering != 1 {
		t.Fatalf("global steering artifacts = %d, want exactly one", steering)
	}

	// The bytes are the embedded corpus, byte-for-byte.
	for _, a := range artifacts {
		for _, m := range embedded.Manifest() {
			if m.Target != a.ID {
				continue
			}
			data, err := fs.ReadFile(embedded.FS, m.Source)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != string(a.Data) {
				t.Errorf("artifact %s bytes diverge from the embedded corpus", a.ID)
			}
		}
	}
}

// TestGlobalClaimsMirrorArtifacts proves classification and projection read the
// same resource set, so a claim can never describe a resource the engine would
// not write.
func TestGlobalClaimsMirrorArtifacts(t *testing.T) {
	root := "/home/u/.claude"
	artifacts, err := GlobalArtifacts(root)
	if err != nil {
		t.Fatalf("GlobalArtifacts: %v", err)
	}
	claims := GlobalClaims(root)

	byID := map[string]managedfile.Claim{}
	for _, c := range claims {
		byID[c.ID] = c
	}
	for _, a := range artifacts {
		c, ok := byID[a.ID]
		if !ok {
			t.Fatalf("artifact %s has no claim", a.ID)
		}
		if c.Kind != a.Kind || c.Path != a.Path {
			t.Errorf("claim %+v disagrees with artifact %+v", c, a)
		}
	}
	if len(claims) != len(artifacts) {
		t.Errorf("claims = %d, artifacts = %d", len(claims), len(artifacts))
	}

	// The steering claim is independent of the whole-file claims, exactly as
	// SelectionClaims excludes the global steering target.
	steering := SteeringClaim(root)
	if steering.Kind != managedfile.KindBlock || filepath.Base(steering.Path) != bundlespec.GlobalSteering.ClaudeTarget {
		t.Errorf("steering claim = %+v", steering)
	}
}

func TestGlobalArtifactsRejectsEmptyRoot(t *testing.T) {
	if _, err := GlobalArtifacts("  "); err == nil {
		t.Error("empty native root accepted")
	}
}
