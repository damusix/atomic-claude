package harness

import "github.com/damusix/atomic-claude/atomic/internal/managedfile"

// Ownership evidence is judged by managedfile, the layer that owns observations
// and managed blocks, and re-exported here because it is part of the adapter
// contract: an adapter produces the claims, and only an approved plan may
// commit what they prove.
type (
	// Evidence is the observed basis for an ownership claim.
	Evidence = managedfile.Evidence
	// Claim is one resource a caller asserts a target owns.
	Claim = managedfile.Claim
	// Assessment is the verdict for one claim against the current native bytes.
	Assessment = managedfile.Assessment
)

// Evidence bases, re-exported for the adapter contract.
const (
	EvidenceSelection = managedfile.EvidenceSelection
	EvidenceBlock     = managedfile.EvidenceBlock
	EvidenceMissing   = managedfile.EvidenceMissing
	EvidenceUnowned   = managedfile.EvidenceUnowned
	EvidenceConflict  = managedfile.EvidenceConflict
)

// Assess judges one claim against the current native bytes, read-only.
func Assess(c Claim) (Assessment, error) { return managedfile.Assess(c) }

// AssessAll judges every claim in order.
func AssessAll(claims []Claim) ([]Assessment, error) { return managedfile.AssessAll(claims) }
