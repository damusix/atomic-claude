package omp

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// SteeringBlock wraps the renderer's OMP steering composition in the managed
// block, so the profile's guidance outside the block stays byte-identical.
func SteeringBlock(cat *artifacts.Catalog) ([]byte, error) {
	projection, err := artifacts.NewRenderer(cat).OMPSteering()
	if err != nil {
		return nil, fmt.Errorf("omp: compose steering: %w", err)
	}
	return managedfile.BlockDocument(projection.Bytes), nil
}

// SteeringDigest returns the digest of the Atomic block alone, never the whole
// file, because the user owns every byte outside the block.
func SteeringDigest(block []byte) (string, error) {
	return managedfile.DigestResourceBytes(block, managedfile.KindBlock)
}
