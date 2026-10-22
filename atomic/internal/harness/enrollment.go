package harness

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/config"
	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// EnrollmentRequest is the caller's explicit intent to enroll one target.
type EnrollmentRequest struct {
	Target Target  `json:"target"`
	Claims []Claim `json:"claims"`
	// Approved carries the user's consent to the plan that establishes
	// ownership. Claims are assessed read-only either way; only an approved plan
	// may claim and record what it found.
	Approved bool `json:"approved"`
}

// Enrollment is the outcome of Enroll.
type Enrollment struct {
	Target      Target       `json:"target"`
	Assessments []Assessment `json:"assessments"`
	// Adopted names the resources whose ownership was established and recorded.
	Adopted []string `json:"adopted,omitempty"`
}

// Enroll assesses every claim and, only inside an approved plan, records the
// target's enrollment and the ownership of every verified resource in the
// ledger at ~/.atomic/install/ledger.json. It writes no native bytes, so it can
// only ever record what it verified; conflicting evidence refuses the whole
// enrollment and leaves the resource for explicit resolution.
//
// The caller serializes this against other lifecycle operations by holding the
// advisory lock at ~/.atomic/install/operation.lock: enrollment is one ledger
// write inside a larger operation, and acquiring the lock here would deadlock
// the operation that already holds it.
func Enroll(home string, req EnrollmentRequest) (Enrollment, error) {
	target := req.Target
	if !target.Kind.Valid() {
		return Enrollment{}, fmt.Errorf("harness: enroll: unknown harness %q", target.Kind)
	}
	if target.Instance == "" {
		return Enrollment{}, fmt.Errorf("harness: enroll %s: instance identity is empty", target.Kind)
	}
	if home == "" {
		return Enrollment{}, fmt.Errorf("harness: enroll %s: no home to record enrollment under", target.Key())
	}

	assessments, err := AssessAll(req.Claims)
	if err != nil {
		return Enrollment{}, err
	}
	out := Enrollment{Target: target, Assessments: assessments}
	for _, a := range assessments {
		if a.Evidence == EvidenceConflict {
			return out, fmt.Errorf("harness: enroll %s: resource %s: %w", target.Key(), a.Claim.ID, ErrEvidenceConflict)
		}
	}
	if !req.Approved {
		return out, fmt.Errorf("harness: enroll %s: %w", target.Key(), ErrPlanNotApproved)
	}

	path := config.LedgerPath(home)
	ledger, err := installstate.LoadLedger(path)
	if err != nil {
		return out, err
	}
	target.Status = StatusEnrolled
	changed := ledger.UpsertTarget(installstate.TargetRecord{
		Harness:    string(target.Kind),
		Instance:   target.Instance,
		NativeRoot: target.NativeRoot,
		Status:     string(target.Status),
	})
	for _, a := range assessments {
		if !a.Owned {
			continue
		}
		out.Adopted = append(out.Adopted, a.Claim.ID)
		if ledger.Upsert(ownershipRow(target, a)) {
			changed = true
		}
	}
	if changed {
		if err := ledger.Save(path); err != nil {
			return out, err
		}
	}
	out.Target = target
	return out, nil
}

// ownershipRow maps one verified claim to the ledger row that records it. The
// applied value carries the observed digest — the block's digest for a managed
// block, the whole file's for a selection-bytes claim — because that is the
// digest later verification compares against.
func ownershipRow(t Target, a Assessment) installstate.Row {
	return installstate.Row{
		Target:   t.Key(),
		Resource: a.Claim.ID,
		Consumer: t.Key(),
		Applied: installstate.AppliedValue{
			Path:   a.Claim.Path,
			Kind:   a.Claim.Kind,
			Digest: a.Observed.Digest,
		},
	}
}
