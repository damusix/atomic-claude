package codex

import (
	"fmt"
	"path/filepath"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// SteeringPath is the user-level AGENTS.md Codex reads into every session; the
// adapter owns one block in it and none of the user's bytes.
func SteeringPath(root string) string { return filepath.Join(root, "AGENTS.md") }

// SteeringBlock returns the Atomic block written into SteeringPath: the steering
// body without its `@` import lines and with the output style always on.
func SteeringBlock(cat *artifacts.Catalog) ([]byte, error) {
	projection, err := artifacts.NewRenderer(cat).CodexSteering()
	if err != nil {
		return nil, fmt.Errorf("codex: compose steering: %w", err)
	}
	return managedfile.BlockDocument(projection.Bytes), nil
}

func steeringDigest(block []byte) (string, error) {
	return managedfile.DigestResourceBytes(block, managedfile.KindBlock)
}
