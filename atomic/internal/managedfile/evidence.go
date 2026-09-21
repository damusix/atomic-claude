package managedfile

// Evidence is the observed basis for an ownership claim over a managed
// resource. Three bases prove ownership: bytes that match the selected binary's
// embedded generation exactly, a resource that carries exactly one parseable
// Atomic block, and bytes the ledger recorded Atomic itself applying. A
// resource name, a legacy config path, and a legacy version string prove
// nothing.
type Evidence string

const (
	// EvidenceSelection means the observed bytes match the selected
	// generation's exact embedded bytes.
	EvidenceSelection Evidence = "selection-bytes"
	// EvidenceBlock means the resource carries exactly one parseable Atomic
	// block. The block is independently verifiable because migration can
	// preserve and replace only that region.
	EvidenceBlock Evidence = "managed-block"
	// EvidenceNoBlock means a block resource exists and carries user bytes but
	// has never held an Atomic block at all. That is "no block yet", not
	// ambiguity: adoption appends a block and preserves every existing byte.
	// A file that carries tags which do not parse stays EvidenceConflict.
	EvidenceNoBlock Evidence = "no-managed-block"
	// EvidenceLedger means the observed bytes equal the digest the ledger
	// recorded applying to this (target, resource). It is proof Atomic wrote
	// them, even when they are an older generation's bytes, and is distinct
	// from a historical digest catalog: it records only what Atomic itself
	// last wrote, not every generation it ever shipped.
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

// Claim is one resource a caller asserts a target owns. ID names the physical
// resource; SelectedDigest is the digest of the exact bytes the selected binary
// would write and is empty for a claim whose evidence is its own managed block.
type Claim struct {
	ID   string `json:"id"`
	Kind Kind   `json:"kind"`
	Path string `json:"path"`
	// SelectedDigest is the selected generation's expected digest. An empty
	// digest proves nothing, so a block claim uses block evidence instead.
	SelectedDigest string `json:"selected_digest,omitempty"`
	// RecordedDigest is the digest the ledger recorded applying to this
	// resource for its target, when one exists. Only an exact match proves
	// Atomic's own write; an empty digest proves nothing.
	RecordedDigest string `json:"recorded_digest,omitempty"`
}

// Assessment is the verdict for one claim against the current native bytes. It
// records evidence, not a claim: only an approved plan may commit what it
// found, so a pre-plan check and an approved plan read the same verdict and
// differ only in whether ownership is established.
type Assessment struct {
	Claim    Claim       `json:"claim"`
	Evidence Evidence    `json:"evidence"`
	Observed Observation `json:"observed"`
	// Owned is true only for selection-bytes and managed-block evidence.
	Owned bool `json:"owned"`
}

// Assess judges one claim against the current native bytes, read-only. It
// reports the evidence it found and never claims ownership from a name: a
// resource that is absent, different, or unreadable stays unowned.
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
		// The file exists and carries user bytes, but has never held an Atomic
		// block: a block that has never existed is not an ambiguous one, so the
		// file is adopted by appending.
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
