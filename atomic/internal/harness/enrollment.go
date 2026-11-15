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
	// Approved carries the user's consent; without it claims are only assessed,
	// never recorded.
	Approved bool `json:"approved"`
}

// Enrollment is the outcome of Enroll.
type Enrollment struct {
	Target      Target       `json:"target"`
	Assessments []Assessment `json:"assessments"`
	// Adopted names the resources whose ownership was established and recorded.
	Adopted []string `json:"adopted,omitempty"`
}

// Enroll records an approved target and its verified resources in the ledger.
// The caller must hold the lifecycle lock; taking it here would deadlock.
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

// ownershipRow records the observed digest as applied, because later
// verification compares against exactly that digest.
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

// EnrolledKinds lists the kinds the install ledger enrolls, in kind order.
func EnrolledKinds(home string) ([]Kind, error) {
	ledger, err := installstate.LoadLedger(config.LedgerPath(home))
	if err != nil {
		return nil, err
	}
	present := map[Kind]bool{}
	for _, t := range ledger.Targets {
		present[Kind(t.Harness)] = true
	}
	var kinds []Kind
	for _, k := range []Kind{KindClaude, KindCodex, KindOMP} {
		if present[k] {
			kinds = append(kinds, k)
		}
	}
	return kinds, nil
}
