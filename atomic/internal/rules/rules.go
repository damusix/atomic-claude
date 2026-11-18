// Package rules parses the path-scoped rule corpus into checkout-independent
// records, binds them to a project, matches paths, and maps them to target scopes.
package rules

import (
	"path/filepath"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
)

// Producer is the family that authored a record. Identity is producer-qualified
// so two producers can never collide by ID.
type Producer string

const (
	// ProducerShipped is the authored context/rules/** corpus.
	ProducerShipped Producer = "shipped"
	// ProducerWiki is repository wiki refresh, which writes pipeline-owned
	// pointer cards under <state-root>/rules/wiki/**.
	ProducerWiki Producer = "wiki"
)

// Class is the record class, which fixes how a target treats it.
type Class string

const (
	// ClassShipped is an authored path-scoped rule.
	ClassShipped Class = "shipped"
	// ClassWikiPointer is a repository-refresh-generated domain card.
	ClassWikiPointer Class = "wiki-pointer"
)

// BaseKind anchors a record's scope to a runtime base. It is never an absolute
// path: one record binds to many checkouts.
type BaseKind string

const (
	// BaseRepositoryRoot is the repository (or worktree) root.
	BaseRepositoryRoot BaseKind = "repository-root"
)

// RuleRecord is one parsed rule source. Nothing here names a checkout.
type RuleRecord struct {
	// ID is the producer-qualified identity: "shipped:<source>" or
	// "wiki:<project-key>:<domain>".
	ID string
	// Class fixes how a target treats this record.
	Class Class
	// Producer is the authoring family, derivable from the ID prefix.
	Producer Producer
	// Source is the producer-relative slash path, e.g.
	// "rules/typescript/style.md". It never carries an absolute path.
	Source string
	// BaseKind anchors scope resolution at match time.
	BaseKind BaseKind
	// Include is the ordered positive globs from `paths:`; it is never empty
	// for a scoped rule.
	Include []string
	// Body is the Markdown bytes with frontmatter removed.
	Body []byte
	// SourceDigest is the digest of the canonical source bytes. It is identical
	// for the same source in two repositories or worktrees.
	SourceDigest string
}

// NativeName is the record's identity relative to a target's rule root. Validate
// rejects two distinct IDs that share one.
func (r RuleRecord) NativeName() string {
	if r.Producer == ProducerWiki {
		return "atomic/wiki/" + strings.TrimPrefix(r.Source, "rules/wiki/")
	}
	return strings.TrimPrefix(r.Source, "rules/")
}

// RuleInstance binds one immutable RuleRecord to a project key and an absolute,
// symlink-resolved runtime base.
type RuleInstance struct {
	Record     RuleRecord
	ProjectKey string
	// Base is the absolute, symlink-resolved runtime base selected by
	// Record.BaseKind.
	Base string
	// Projections is the per-target projection state an adapter records after
	// materializing this instance. It is empty until a target projects it.
	Projections []Projection
}

// Match is one record matched against a candidate path, with the include glob
// that matched and the instance base that resolved it.
type Match struct {
	Record RuleRecord
	Base   string
	Glob   string
}

// RuntimeDelivery is how a target supplies a matched record at operation time.
type RuntimeDelivery string

const (
	// RuntimeStaticScope: the target's proven native scoped-rule surface
	// selects and supplies the record with no Atomic runtime involvement.
	RuntimeStaticScope RuntimeDelivery = "static-scope"
	// RuntimePreOperation: a proven pre-operation event extracts the target
	// path and Atomic supplies the matched body as model guidance.
	RuntimePreOperation RuntimeDelivery = "pre-operation"
	// RuntimeNone: no proven role reproduces the behavior.
	RuntimeNone RuntimeDelivery = "none"
)

// ScopeField maps a record's include patterns onto one native scope surface.
// Field is "paths" for native scope, or the proven pre-operation event name.
type ScopeField struct {
	Field    string
	Patterns []string
}

// Projection is the target-native mapping of one record, built from capability
// evidence alone with no target code.
type Projection struct {
	RecordID string
	Target   artifacts.Target
	// Scope is empty when the delivery is RuntimeNone.
	Scope    []ScopeField
	Delivery RuntimeDelivery
	// Tier is native-scope, hook-required, or unsupported; a prose record never
	// selects deny-enforced.
	Tier artifacts.EnforcementTier
	// NativeResource is the record's identity relative to the target's rule
	// root.
	NativeResource string
}

// resolveSymlinks gives path's absolute, symlink-free form, so two checkouts
// share a base only when the filesystem resolves them to the same location.
func resolveSymlinks(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(abs))
	if err != nil {
		return "", err
	}
	return resolved, nil
}
