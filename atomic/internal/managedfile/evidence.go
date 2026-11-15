package managedfile

// Evidence is the observed basis for an ownership claim over a managed resource.
// A resource name, a legacy config path, or a legacy version string proves nothing.
type Evidence string

const (
	// EvidenceSelection means the observed bytes match the selected
	// generation's exact embedded bytes.
	EvidenceSelection Evidence = "selection-bytes"
	// EvidenceBlock means the resource carries exactly one parseable Atomic
	// block, which migration can preserve and replace on its own.
	EvidenceBlock Evidence = "managed-block"
	// EvidenceNoBlock means a block resource holds user bytes and never held an
	// Atomic block, so adoption appends one. Unparseable tags stay EvidenceConflict.
	EvidenceNoBlock Evidence = "no-managed-block"
	// EvidenceLedger means the observed bytes equal the digest the ledger recorded
	// Atomic last writing to this resource, even an older generation's bytes.
	EvidenceLedger Evidence = "ledger-applied"
	// EvidenceMissing means the resource is listed but absent. That is drift to
	// be recreated after convergence is accepted, never ownership.
	EvidenceMissing Evidence = "missing"
	// EvidenceUnowned means nothing verifiable was found. The bytes are
	// preserved and a per-resource replace-or-leave-unowned decision is needed.
	EvidenceUnowned Evidence = "unowned"
	// EvidenceConflict means the evidence is ambiguous or malformed and blocks
	// automatic adoption.
	EvidenceConflict Evidence = "conflict"
)

// Claim is one resource a caller asserts a target owns.
type Claim struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	Path string `json:"path"`
	// SelectedDigest is the selected generation's expected digest. An empty
	// digest proves nothing, so a block claim uses block evidence instead.
	SelectedDigest string `json:"selected_digest,omitempty"`
	// RecordedDigest is the digest the ledger recorded applying to this resource.
	// Only an exact match proves Atomic's own write.
	RecordedDigest string `json:"recorded_digest,omitempty"`
}

// Assessment is the verdict for one claim against the current native bytes. It
// records evidence only; an approved plan is what commits ownership.
type Assessment struct {
	Claim    Claim       `json:"claim"`
	Evidence Evidence    `json:"evidence"`
	Observed Observation `json:"observed"`
	// Owned is true for selection-bytes, managed-block, and ledger-applied evidence.
	Owned bool `json:"owned"`
}

// Assess judges one claim against the current native bytes, read-only. An
// absent, different, or unreadable resource stays unowned.
func Assess(c Claim) (Assessment, error) {
	obs, err := Observe(c.Path, c.Kind)
	if err != nil {
		return Assessment{}, err
	}
	a := Assessment{Claim: c, Observed: obs}
	switch {
	case !obs.Exists():
		a.Evidence = EvidenceMissing
	case c.Kind == KindBlock && obs.Conflict == ConflictMalformedBlock && !HasBlockTags(obs.Bytes):
		a.Evidence = EvidenceNoBlock
	case obs.Conflict != ConflictNone:
		a.Evidence = EvidenceConflict
	case c.Kind == KindBlock:
		a.Evidence, a.Owned = EvidenceBlock, true
		if c.SelectedDigest != "" && obs.Digest == c.SelectedDigest {
			a.Evidence = EvidenceSelection
		}
	case c.SelectedDigest != "" && obs.Digest == c.SelectedDigest:
		a.Evidence, a.Owned = EvidenceSelection, true
	case c.RecordedDigest != "" && obs.Digest == c.RecordedDigest:
		a.Evidence, a.Owned = EvidenceLedger, true
	default:
		a.Evidence = EvidenceUnowned
	}
	return a, nil
}

// AssessAll judges every claim in order.
func AssessAll(claims []Claim) ([]Assessment, error) {
	out := make([]Assessment, 0, len(claims))
	for _, c := range claims {
		a, err := Assess(c)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}
