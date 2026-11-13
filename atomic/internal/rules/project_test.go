package rules

import (
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
)

func TestSelect_TierFromEvidence(t *testing.T) {
	rec := loadOne(t)

	cases := []struct {
		name     string
		ev       Evidence
		delivery RuntimeDelivery
		tier     artifacts.EnforcementTier
		field    string
	}{
		{
			"native scoped-rule surface",
			Evidence{StaticScope: true},
			RuntimeStaticScope,
			artifacts.EnforcementNativeScope,
			"paths",
		},
		{
			"pre-operation event with context return",
			Evidence{PreOperation: true, ContextReturn: true, Event: "tool_call"},
			RuntimePreOperation,
			artifacts.EnforcementHookRequired,
			"tool_call",
		},
		{
			"pre-operation event without context return",
			Evidence{PreOperation: true, Event: "tool_call"},
			RuntimeNone,
			artifacts.EnforcementUnsupported,
			"",
		},
		{
			"no proven role",
			Evidence{},
			RuntimeNone,
			artifacts.EnforcementUnsupported,
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Select(rec, artifacts.TargetClaude, tc.ev)
			if p.Tier != tc.tier || p.Delivery != tc.delivery {
				t.Errorf("delivery/tier = %q/%q, want %q/%q", p.Delivery, p.Tier, tc.delivery, tc.tier)
			}
			if p.RecordID != rec.ID || p.NativeResource != rec.NativeName() {
				t.Errorf("projection does not name record identity: %+v", p)
			}
			if tc.field == "" {
				if len(p.Scope) != 0 {
					t.Errorf("unsupported projection carries scope %+v", p.Scope)
				}
				return
			}
			if len(p.Scope) != 1 || p.Scope[0].Field != tc.field {
				t.Fatalf("scope = %+v, want field %q", p.Scope, tc.field)
			}
			if got := len(p.Scope[0].Patterns); got != len(rec.Include) {
				t.Errorf("scope patterns = %d, want %d", got, len(rec.Include))
			}
		})
	}
}

// The projection copies scope patterns, so a target cannot mutate the record.
func TestSelect_CopiesPatterns(t *testing.T) {
	rec := loadOne(t)
	p := Select(rec, artifacts.TargetClaude, Evidence{StaticScope: true})
	p.Scope[0].Patterns[0] = "mutated"
	if rec.Include[0] == "mutated" {
		t.Error("projection aliases the record's include patterns")
	}
}

// The target is carried through untouched, so one record can project to several
// targets without sharing tier state.
func TestSelect_CarriesTarget(t *testing.T) {
	rec := loadOne(t)
	for _, target := range []artifacts.Target{artifacts.TargetClaude, artifacts.TargetOMP, artifacts.TargetCodex} {
		if p := Select(rec, target, Evidence{}); p.Target != target {
			t.Errorf("Target = %q, want %q", p.Target, target)
		}
	}
}
