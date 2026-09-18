// Package artifacts renders the authored context/ corpus once into canonical
// artifacts, then projects them into target-native files.
//
// Rendering is offline and deterministic: the same sources produce identical
// bytes and identical digests on every run. Partials are expanded exactly once,
// while the corpus is built — a projection never re-renders a body.
package artifacts

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
)

// Kind is the canonical artifact family.
type Kind string

const (
	KindSteering    Kind = "steering"
	KindAgent       Kind = "agent"
	KindSkill       Kind = "skill"
	KindOutputStyle Kind = "output-style"
	KindCommand     Kind = "command"
	KindRule        Kind = "rule"
)

// Target is a native harness an adapter projects into.
type Target string

const (
	TargetClaude Target = "claude"
	TargetOMP    Target = "omp"
	TargetCodex  Target = "codex"
)

// Delivery classifies how a target consumes a projection.
type Delivery string

const (
	// DeliveryDirect: the rendered body is written verbatim to one native file.
	DeliveryDirect Delivery = "direct"
	// DeliveryShared: the shared steering source itself, which a target loads
	// through an adjacent loader.
	DeliveryShared Delivery = "shared"
	// DeliveryLoader: a thin native file whose only content imports an adjacent
	// steering source.
	DeliveryLoader Delivery = "loader"
	// DeliveryComposed: one native file assembled from more than one rendered
	// body.
	DeliveryComposed Delivery = "composed"
)

// EnforcementTier is the strongest native guarantee a projection can claim for
// the behavior it carries. A target that has no proven capability row is
// unsupported: the content is instruction-only and never reads as a native
// boundary.
type EnforcementTier string

const (
	// EnforcementNativeScope: the target delivers the behavior through a proven
	// native surface with no Atomic runtime involvement.
	EnforcementNativeScope EnforcementTier = "native-scope"
	// EnforcementHookRequired: Atomic deterministically supplies the behavior
	// before an operation, but the prose remains model guidance.
	EnforcementHookRequired EnforcementTier = "hook-required"
	// EnforcementDenyEnforced: an Atomic hook blocks an operation on an exact
	// machine predicate.
	EnforcementDenyEnforced EnforcementTier = "deny-enforced"
	// EnforcementUnsupported: no native guarantee is proven.
	EnforcementUnsupported EnforcementTier = "unsupported"
)

// Semantics is the portable metadata an adapter reads instead of re-parsing an
// artifact: identity, description, and the canonical artifacts this one
// depends on.
type Semantics struct {
	Name        string
	Description string
	Requires    []string
	// Malformed reports authored frontmatter that did not parse as YAML. The
	// body still ships byte-for-byte; corpus validation is where malformed
	// metadata fails.
	Malformed bool
}

// Artifact is one canonical corpus entry: a checkout-independent identity, the
// authored source it came from, the bytes that ship, and the semantics a target
// adapter needs.
type Artifact struct {
	// ID is the checkout-independent identity, "<kind>:<source>".
	ID   string
	Kind Kind
	// Source is the context/-relative path, slash-separated.
	Source string
	// Body is the authored bytes with partials expanded exactly once.
	Body      []byte
	Semantics Semantics
	// SourceDigest is the digest of the authored source bytes.
	SourceDigest string
}

// Projection is a target-native rendering of canonical bytes.
type Projection struct {
	// Artifact is the canonical ID this projection came from; empty when the
	// projection is assembled from more than one artifact.
	Artifact string
	Target   Target
	// Path is the native path relative to the target root, slash-separated.
	Path     string
	Bytes    []byte
	Delivery Delivery
	// Enforcement is the strongest native guarantee the target's capability
	// record proves for this projection.
	Enforcement EnforcementTier
	// Unsupported lists, by stable identity, the canonical fields this
	// projection could not carry natively. A degraded projection reports what it
	// dropped instead of implying the target supports it; empty means the
	// projection carries every canonical field.
	Unsupported []string
	// Digest is the digest of Bytes.
	Digest string
}

// ProjectionDigest is the one checksum form for projected bytes, shared with the
// managed-file primitives that verify native writes.
func ProjectionDigest(data []byte) string {
	return managedfile.Digest(data)
}

// SkillID is the canonical identity of a skill's SKILL.md, the form an agent's
// skills: metadata resolves to.
func SkillID(name string) string {
	return string(KindSkill) + ":skills/" + name + "/SKILL.md"
}

// AgentBody returns a canonical agent's instruction body: its rendered bytes
// with the portable frontmatter excluded and the result normalized to exactly
// one trailing newline. Native metadata travels as the target's own fields, so
// every projection preserves this body and Codex's developer_instructions
// round-trips it byte-for-byte.
func AgentBody(a Artifact) ([]byte, error) {
	if a.Kind != KindAgent {
		return nil, fmt.Errorf("artifacts: %s is not an agent", a.ID)
	}
	_, body, err := frontmatter.Parse(string(a.Body))
	if err != nil {
		return nil, fmt.Errorf("artifacts: %s frontmatter: %w", a.ID, err)
	}
	return terminate([]byte(strings.TrimLeft(body, "\n"))), nil
}

// scopedPath joins a target-native directory with a file name, treating an
// empty or "." directory as the scope root.
func scopedPath(dir, name string) string {
	return filepath.ToSlash(filepath.Join(dir, name))
}
