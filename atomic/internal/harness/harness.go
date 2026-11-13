// Package harness owns the adapter contract, the adapter registry, and the
// enrollment target model. See docs/spec/omp-plugin-compatibility.md.
package harness

import (
	"errors"
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/harness/kind"
)

// Kind names a supported harness; it lives in the kind leaf package so hooks
// can name one without importing harness.
type Kind = kind.Kind

const (
	KindClaude = kind.Claude
	KindOMP    = kind.OMP
	KindCodex  = kind.Codex
)

// ParseKind validates a user-supplied harness name.
func ParseKind(name string) (Kind, error) {
	k := Kind(strings.ToLower(strings.TrimSpace(name)))
	if !k.Valid() {
		return "", fmt.Errorf("harness: unknown harness %q", name)
	}
	return k, nil
}

// Instance is one discovered native installation. Discovery never enrolls, so
// an instance carries no ownership.
type Instance struct {
	Kind Kind `json:"kind"`
	// ID is the stable instance identity. Path-rooted harnesses use the native
	// root's path, so two profiles of one harness are two instances.
	ID         string `json:"id"`
	NativeRoot string `json:"native_root"`
	// Home is the OS home discovery ran under. It scopes the instance but is not
	// part of its identity.
	Home string `json:"home,omitempty"`
	// Exists reports whether the native root was present on disk. An explicitly
	// configured root that does not exist yet is still an instance.
	Exists bool `json:"exists"`
}

// Target returns the unenrolled target this instance describes.
func (i Instance) Target() Target {
	return Target{Kind: i.Kind, Instance: i.ID, NativeRoot: i.NativeRoot}
}

// Adapter is the contract one harness implements.
type Adapter interface {
	// Kind is the harness this adapter serves.
	Kind() Kind
	// Discover reports every native instance visible from home. It must not
	// write anything, not even a cache.
	Discover(home string) ([]Instance, error)
	// Capabilities returns the proven capability record for the tested
	// harness version.
	Capabilities() CapabilityMatrix
	// Lifecycle returns the hook points for one home, so a hook can reach the
	// ledger without the caller threading it through.
	Lifecycle(home string) Lifecycle
}

// Errors every adapter shares.
var (
	// ErrUnsupported reports a lifecycle surface the adapter has not wired. A
	// missing hook is an honest capability report, never a silent success.
	ErrUnsupported = errors.New("harness: capability unsupported")
	// ErrPlanNotApproved reports ownership attempted outside an approved plan.
	ErrPlanNotApproved = errors.New("harness: ownership requires an approved plan")
	// ErrEvidenceConflict reports native bytes whose ownership cannot be
	// established; the resource is preserved, never guessed at.
	ErrEvidenceConflict = errors.New("harness: ownership evidence conflicts")
	// ErrSharedResource reports removal of infrastructure another enrolled
	// consumer still depends on.
	ErrSharedResource = errors.New("harness: resource is shared by another consumer")
	// ErrNoInstance reports a selector that matched nothing.
	ErrNoInstance = errors.New("harness: no matching instance")
	// ErrAmbiguousSelection reports a selector that matched more than one
	// instance. It is matched by AmbiguousSelectionError.
	ErrAmbiguousSelection = errors.New("harness: ambiguous instance selection")
	// ErrNotInstalled reports a harness whose native root could not be resolved;
	// installed-harness discovery treats it as "not found" and warns.
	ErrNotInstalled = errors.New("harness: not installed")
)
