package rules

import "github.com/damusix/atomic-claude/atomic/internal/artifacts"

// canonicalScopeField is the authored scope field, which a native-scope target
// copies byte-for-byte.
const canonicalScopeField = "paths"

// Evidence is the proven rule roles Select reads; an unproven role is false. The
// adapter supplies it so this package imports no harness adapter.
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

// Select maps one record onto a target's native scope from capability evidence.
// Deny-enforced is unreachable: a prose record carries no machine predicate.
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
