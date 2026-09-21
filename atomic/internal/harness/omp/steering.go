package omp

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// SteeringBlock returns the Atomic-owned document written into a profile's
// AGENTS.md: one managed block carrying the canonical composition — the
// import-free rendered steering body, one blank line, and the Atomic output
// style with its parsed YAML frontmatter excluded. The composition is the CP2A
// OMP golden; this function only wraps it in the block boundary so the
// profile's own guidance outside the block stays byte-identical.
func SteeringBlock(cat *artifacts.Catalog) ([]byte, error) {
	projection, err := artifacts.NewRenderer(cat).OMPSteering()
	if err != nil {
		return nil, fmt.Errorf("omp: compose steering: %w", err)
	}
	return managedfile.BlockDocument(projection.Bytes), nil
}

// SteeringDigest returns the ownership digest of the Atomic block: the block
// digest a profile's steering is compared and recorded by, never the whole
// file's, because the user owns every byte outside the block.
func SteeringDigest(block []byte) (string, error) {
	return managedfile.DigestResourceBytes(block, managedfile.KindBlock)
}
