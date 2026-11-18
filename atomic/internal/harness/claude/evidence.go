package claude

import (
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// steeringTarget is the one Claude resource owned as a managed block rather
// than a whole file; steeringKind is its bundle manifest kind.
const (
	steeringTarget = "CLAUDE.md"
	steeringKind   = "claude-md"
	agentKind      = "agent"
)

// SteeringClaim returns the block claim for nativeRoot's global CLAUDE.md, which
// is user-owned prose holding one Atomic block.
func SteeringClaim(nativeRoot string) harness.Claim {
	return harness.Claim{
		ID:   steeringTarget,
		Kind: managedfile.KindBlock,
		Path: filepath.Join(nativeRoot, steeringTarget),
	}
}

// SelectionClaims returns one whole-file claim per bundled artifact under
// nativeRoot, excluding the steering block SteeringClaim owns.
func SelectionClaims(home, nativeRoot string) ([]harness.Claim, error) {
	artifacts, err := GlobalArtifacts(home, nativeRoot)
	if err != nil {
		return nil, err
	}
	out := make([]harness.Claim, 0, len(artifacts))
	for _, a := range artifacts {
		if a.Kind == managedfile.KindBlock {
			continue
		}
		out = append(out, harness.Claim{
			ID:             a.ID,
			Kind:           managedfile.KindFile,
			Path:           a.Path,
			SelectedDigest: managedfile.Digest(a.Data),
		})
	}
	return out, nil
}
