package harness

import (
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// Status is the convergence state enrollment recorded for one target. Only the
// enrolled state is written by this checkpoint; the remaining values are the
// reporting vocabulary the convergence and verification hooks use.
type Status string

const (
	// StatusEnrolled means the target is explicitly enrolled and no convergence
	// result has been recorded yet.
	StatusEnrolled Status = "enrolled"
	// StatusConverged means the last convergence verified the target's effective
	// content.
	StatusConverged Status = "converged"
	// StatusStale means canonical authority moved ahead of this target's
	// projection.
	StatusStale Status = "stale"
	// StatusConflicted means native bytes changed outside Atomic's last applied
	// digest, so the target is preserved for resolution.
	StatusConflicted Status = "conflicted"
)

// Target is one harness instance Atomic operates on: the kind, its stable
// instance identity, the native root those bytes live under, and the
// convergence status enrollment recorded. A Target is created by discovery but
// persisted only by enrollment.
type Target struct {
	Kind       Kind   `json:"harness"`
	Instance   string `json:"instance"`
	NativeRoot string `json:"native_root"`
	Status     Status `json:"status,omitempty"`
}

// Key is the target's durable identity: kind and instance, the pair the ledger
// keys target records and ownership rows by. The ledger owns the spelling, so
// this delegates rather than keeping a second copy of it.
func (t Target) Key() string {
	return installstate.TargetRecord{Harness: string(t.Kind), Instance: t.Instance}.Key()
}

func (t Target) String() string { return t.Key() }

// ParseKey splits a ledger target key back into a target. Only identity is
// recoverable from a key: the native root and convergence status come from the
// ledger's target record.
func ParseKey(key string) (Target, error) {
	kind, instance, ok := strings.Cut(key, ":")
	if !ok || kind == "" || instance == "" {
		return Target{}, fmt.Errorf("harness: malformed target key %q", key)
	}
	k, err := ParseKind(kind)
	if err != nil {
		return Target{}, fmt.Errorf("harness: malformed target key %q: %w", key, err)
	}
	return Target{Kind: k, Instance: instance}, nil
}
