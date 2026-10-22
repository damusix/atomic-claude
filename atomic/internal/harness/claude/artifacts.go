package claude

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// GlobalSteeringPath is Claude's direct user-level steering file. The global
// contract renders straight into CLAUDE.md: there is no user-level AGENTS.md and
// no global loader.
func GlobalSteeringPath(nativeRoot string) string {
	return filepath.Join(nativeRoot, steeringTarget)
}

// GlobalLoaderPath is the user-level AGENTS.md a Claude install must never
// create. The adapter exposes it so migration can assert its absence instead of
// assuming it.
func GlobalLoaderPath(nativeRoot string) string {
	return filepath.Join(nativeRoot, "AGENTS.md")
}

// GlobalArtifacts returns the selected generation's adoption artifacts for a
// Claude native root, read from the binary's embedded corpus. Global steering is
// a managed block — Claude's file carries user prose around it — and every other
// artifact is a whole file. The manifest's own target mapping is the projection:
// the global steering source resolves to CLAUDE.md, never to an AGENTS.md, and a
// corpus that maps any artifact there is refused rather than written.
func GlobalArtifacts(nativeRoot string) ([]installstate.Artifact, error) {
	if strings.TrimSpace(nativeRoot) == "" {
		return nil, fmt.Errorf("claude: global artifacts need a native root")
	}

	manifest := embedded.Manifest()
	out := make([]installstate.Artifact, 0, len(manifest))
	steering := false
	for _, a := range manifest {
		if a.Target == "" {
			return nil, fmt.Errorf("claude: embedded artifact %s carries no target", a.Source)
		}
		if a.Target == filepath.ToSlash(filepath.Base(GlobalLoaderPath(nativeRoot))) {
			return nil, fmt.Errorf("claude: embedded artifact %s projects to the forbidden global loader %s", a.Source, a.Target)
		}
		data, err := fs.ReadFile(embedded.FS, a.Source)
		if err != nil {
			return nil, fmt.Errorf("claude: read embedded %s: %w", a.Source, err)
		}
		kind := managedfile.KindFile
		if a.Kind == steeringKind {
			kind = managedfile.KindBlock
			steering = true
		}
		out = append(out, installstate.Artifact{
			ID:   a.Target,
			Kind: kind,
			Path: filepath.Join(nativeRoot, filepath.FromSlash(a.Target)),
			Data: data,
		})
	}
	if !steering {
		return nil, fmt.Errorf("claude: embedded corpus carries no global steering artifact")
	}
	return out, nil
}

// GlobalClaims returns the ownership claims the selected generation makes under
// nativeRoot: the managed global-steering block plus one whole-file claim per
// remaining artifact. It is the read-only classification input the migration
// engine hands to installstate, and it mirrors GlobalArtifacts exactly.
func GlobalClaims(nativeRoot string) []managedfile.Claim {
	return append([]managedfile.Claim{SteeringClaim(nativeRoot)}, SelectionClaims(nativeRoot)...)
}
