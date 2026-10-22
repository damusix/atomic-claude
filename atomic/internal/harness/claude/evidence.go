package claude

import (
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/embedded"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// steeringTarget is the global steering file's path inside a Claude root, and
// steeringKind is its bundle manifest kind. It is the one resource whose
// ownership is a managed block rather than a whole file.
const (
	steeringTarget = "CLAUDE.md"
	steeringKind   = "claude-md"
)

// SteeringClaim returns the ownership claim for nativeRoot's global CLAUDE.md.
// Global steering is user-owned prose holding one Atomic block, so its evidence
// is that block, never a whole-file digest.
func SteeringClaim(nativeRoot string) harness.Claim {
	return harness.Claim{
		ID:   steeringTarget,
		Kind: managedfile.KindBlock,
		Path: filepath.Join(nativeRoot, steeringTarget),
	}
}

// SelectionClaims returns the ownership claims the selected binary's embedded
// generation makes under nativeRoot: one whole-file claim per bundled artifact,
// carrying the digest of the exact bytes this binary would write. The global
// steering artifact is excluded because SteeringClaim owns it as a block.
func SelectionClaims(nativeRoot string) []harness.Claim {
	manifest := embedded.Manifest()
	out := make([]harness.Claim, 0, len(manifest))
	for _, a := range manifest {
		if a.Kind == steeringKind {
			continue
		}
		out = append(out, harness.Claim{
			ID:             a.Target,
			Kind:           managedfile.KindFile,
			Path:           filepath.Join(nativeRoot, filepath.FromSlash(a.Target)),
			SelectedDigest: a.SHA256,
		})
	}
	return out
}
