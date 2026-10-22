package rules

import "github.com/damusix/atomic-claude/atomic/internal/artifacts"

// canonicalScopeField is the authored scope field. A native-scope target
// materializes it byte-for-byte into its scoped-rule surface, so it is the
// native field name there too.
const canonicalScopeField = "paths"

// Evidence is the CP0-proven rule roles Select reads. An adapter derives it
// from its own capability matrix — this package deliberately imports no harness
// adapter, so a projection layer can depend on rules without a cycle. A role
// that is absent, partial, or never exercised is false, so unproven behavior
// can never read as support.
type Evidence struct {
	// StaticScope proves the target's native scoped-rule surface.
	StaticScope bool
	// PreOperation proves a pre-operation event that exposes structured target
	// paths.
	PreOperation bool
	// ContextReturn proves the matched body is available before the operation.
	ContextReturn bool
	// Event is the proven pre-operation event name, used as the scope field for
	// hook-required delivery.
	Event string
}

// Select maps one record onto a target's native scope from CP0 evidence. A
// proven static scoped-rule surface yields native-scope delivery; a proven
// pre-operation event that can also return context yields hook-required
// delivery; anything less is unsupported. A missing row never claims parity and
// never blocks installation — the record simply carries no proven delivery.
//
// The deny-enforced tier is deliberately unreachable here: only an exact
// machine predicate on a mediated operation can deny, and a prose record
// carries none.
func Select(record RuleRecord, target artifacts.Target, ev Evidence) Projection {
	p := Projection{
		RecordID:       record.ID,
		Target:         target,
		NativeResource: record.NativeName(),
	}
	switch {
	case ev.StaticScope:
		p.Delivery = RuntimeStaticScope
		p.Tier = artifacts.EnforcementNativeScope
		p.Scope = []ScopeField{{Field: canonicalScopeField, Patterns: append([]string(nil), record.Include...)}}
	case ev.PreOperation && ev.ContextReturn:
		p.Delivery = RuntimePreOperation
		p.Tier = artifacts.EnforcementHookRequired
		p.Scope = []ScopeField{{
			Field:    ev.Event,
			Patterns: append([]string(nil), record.Include...),
		}}
	default:
		p.Delivery = RuntimeNone
		p.Tier = artifacts.EnforcementUnsupported
	}
	return p
}
