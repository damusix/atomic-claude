package harness

import (
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/installstate"
)

// Status is the convergence state recorded for one target.
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

// Target is one harness instance Atomic operates on. Discovery creates it;
// only enrollment persists it.
type Target struct {
	Kind       Kind   `json:"harness"`
	Instance   string `json:"instance"`
	NativeRoot string `json:"native_root"`
	Status     Status `json:"status,omitempty"`
}

// Key is the target's durable ledger identity. It delegates to the ledger,
// which owns the spelling.
func (t Target) Key() string {
	return installstate.TargetRecord{Harness: string(t.Kind), Instance: t.Instance}.Key()
}

func (t Target) String() string { return t.Key() }

// ParseKey splits a ledger target key back into a target's identity; the native
// root and status come from the ledger's target record.
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
