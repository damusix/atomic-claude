// Package harness owns the adapter contract every supported coding harness
// implements, the registry that resolves one concrete adapter per kind, the
// target model enrollment records, and the ownership-evidence contract that
// decides whether a native resource may be claimed. The evidence rule itself is
// judged by managedfile, beside the observations and managed blocks it reads.
//
// Discovery is read-only. Scanning for instances reports candidates and shared
// visibility and creates no enrollment, ledger, journal, or adoption state; a
// target becomes enrolled only through an approved enrollment plan.
package harness

import (
	"errors"
	"fmt"
	"strings"
)

// Kind names a supported harness. A harness is first-class only for the
// surfaces its milestone promises, so the capability matrix — not the kind —
// records what each one actually delivers.
type Kind string

const (
	KindClaude Kind = "claude"
	KindOMP    Kind = "omp"
	KindCodex  Kind = "codex"
)

// Valid reports whether k is a harness this binary knows.
func (k Kind) Valid() bool {
	switch k {
	case KindClaude, KindOMP, KindCodex:
		return true
	}
	return false
}

// ParseKind validates a user-supplied harness name.
func ParseKind(name string) (Kind, error) {
	k := Kind(strings.ToLower(strings.TrimSpace(name)))
	if !k.Valid() {
		return "", fmt.Errorf("harness: unknown harness %q", name)
	}
	return k, nil
}

// Instance is one discovered native installation: a kind, the native root its
// bytes live under, and whether discovery found that root on disk. Discovery
// observes bytes; it never enrolls, so an instance carries no ownership and no
// consumer.
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

// Adapter is the contract one harness implements: read-only instance discovery,
// the versioned capability record, and the lifecycle hook points the install
// engine drives for one target.
type Adapter interface {
	// Kind is the harness this adapter serves.
	Kind() Kind
	// Discover reports every native instance visible from home. It must not
	// write anything, not even a cache.
	Discover(home string) ([]Instance, error)
	// Capabilities returns the CP0-proven capability record for the tested
	// harness version.
	Capabilities() CapabilityMatrix
	// Lifecycle returns the projection, convergence, verification, and removal
	// hook points this adapter supplies.
	Lifecycle() Lifecycle
}

// Errors every adapter shares. A caller can distinguish a refused ambiguity,
// an unapproved plan, conflicting evidence, a shared resource, and a surface
// the adapter does not implement.
var (
	// ErrUnsupported reports a lifecycle surface the adapter has not wired. A
	// missing hook is an honest capability report, never a silent success.
	ErrUnsupported = errors.New("harness: capability unsupported")
	// ErrPlanNotApproved reports ownership attempted outside an approved plan.
	ErrPlanNotApproved = errors.New("harness: ownership requires an approved plan")
	// ErrEvidenceConflict reports native bytes whose ownership cannot be
	// established: a malformed block or overlapping evidence. The resource is
	// preserved, never guessed at.
	ErrEvidenceConflict = errors.New("harness: ownership evidence conflicts")
	// ErrSharedResource reports removal of infrastructure another enrolled
	// consumer still depends on.
	ErrSharedResource = errors.New("harness: resource is shared by another consumer")
	// ErrNoInstance reports a selector that matched nothing.
	ErrNoInstance = errors.New("harness: no matching instance")
	// ErrAmbiguousSelection reports a selector that matched more than one
	// instance. It is matched by AmbiguousSelectionError.
	ErrAmbiguousSelection = errors.New("harness: ambiguous instance selection")
)
