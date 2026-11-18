package claude

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	atomicartifacts "github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/bundlespec"
	"github.com/damusix/atomic-claude/atomic/internal/claudeinstall"
	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Claude reads only CLAUDE.md at user level, so steering must land there and
// nothing may project to a user-level AGENTS.md.
func TestGlobalArtifactsMapSteeringToClaudeMd(t *testing.T) {
	root := "/home/u/.claude"
	artifacts, err := GlobalArtifacts(t.TempDir(), root)
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

	// The bytes are the embedded corpus, byte-for-byte, except that steering
	// carries the Claude native-vocabulary paragraph inside its block.
	for _, a := range artifacts {
		for _, m := range embedded.Manifest() {
			if m.Target != a.ID {
				continue
			}
			data, err := fs.ReadFile(embedded.FS, m.Source)
			if err != nil {
				t.Fatal(err)
			}
			if a.ID == steeringTarget {
				if data, err = atomicartifacts.ClaudeSteering(data); err != nil {
					t.Fatal(err)
				}
				block, err := managedfile.ManagedBlock(a.Data)
				if err != nil {
					t.Fatal(err)
				}
				if !strings.Contains(string(block), "`subagent_type`") {
					t.Error("the steering block does not name the Agent tool's subagent_type")
				}
			}
			if string(data) != string(a.Data) {
				t.Errorf("artifact %s bytes diverge from the embedded corpus", a.ID)
			}
		}
	}
}

// Doctor's install check diffs through the legacy installer, so the steering the
// adapter writes must read as matching there, or every enrolled root WARNs drift.
func TestAdapterSteeringMatchesInstallDiff(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	artifacts, err := GlobalArtifacts(home, root)
	if err != nil {
		t.Fatalf("GlobalArtifacts: %v", err)
	}
	for _, a := range artifacts {
		if a.ID == steeringTarget {
			if err := os.WriteFile(a.Path, a.Data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	rows, err := claudeinstall.Diff(root, home)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	for _, r := range rows {
		if r.Artifact.Target == steeringTarget && r.Status != claudeinstall.DiffMatch {
			t.Errorf("adapter-written CLAUDE.md diffs as %s, want match", r.Status)
		}
	}
}

// A claim must never describe a resource the engine would not write.
func TestGlobalClaimsMirrorArtifacts(t *testing.T) {
	root := "/home/u/.claude"
	home := t.TempDir()
	artifacts, err := GlobalArtifacts(home, root)
	if err != nil {
		t.Fatalf("GlobalArtifacts: %v", err)
	}
	claims, err := GlobalClaims(home, root)
	if err != nil {
		t.Fatalf("GlobalClaims: %v", err)
	}

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
	if _, err := GlobalArtifacts(t.TempDir(), "  "); err == nil {
		t.Error("empty native root accepted")
	}
}
