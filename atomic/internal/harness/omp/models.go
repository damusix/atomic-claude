package omp

import (
	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// Model role handling is deliberately empty of values. The design keeps model
// policy with the user: an adapter may write a role default only where the
// native surface and its precedence against an existing user setting are
// proven, and never with a concrete provider or model identifier. CP0 exercised
// the profile's config.yml once but never its precedence, so this adapter
// reports the gap and writes nothing.

// ModelRole names one semantic OMP model role. The names come from the CLI
// flags CP0 recorded, which select a model per role; a role name is not a
// provider or model identifier.
type ModelRole string

const (
	// ModelDefault is the role a session uses unless another role is selected.
	ModelDefault ModelRole = "default"
	// ModelSmol is the fast model for lightweight tasks.
	ModelSmol ModelRole = "smol"
	// ModelSlow is the reasoning model for thorough analysis.
	ModelSlow ModelRole = "slow"
	// ModelPlan is the model for architectural planning.
	ModelPlan ModelRole = "plan"
)

// ModelRoles lists the semantic roles OMP exposes, in stable order.
var ModelRoles = []ModelRole{ModelDefault, ModelSmol, ModelSlow, ModelPlan}

// RolePreference is one semantic execution preference: which role it belongs
// to and the preference word itself. It carries no provider or model ID, so a
// caller cannot smuggle a concrete choice through the adapter.
type RolePreference struct {
	Role       ModelRole `json:"role"`
	Preference string    `json:"preference"`
}

// RoleDefault is one resolved role default: the semantic preference and the OMP
// role that would carry it. It has no value field on purpose — the adapter
// never produces a provider or model identifier.
type RoleDefault struct {
	Role       ModelRole `json:"role"`
	Preference string    `json:"preference"`
}

// RoleDefaults resolves semantic preferences onto OMP roles. The mapping is all
// the adapter owns; the authoritative value stays in the profile's config.yml.
// Until CP0 proves the role-default surface, the resolution reports the gap and
// returns no default, so nothing is written into a profile the user owns.
func RoleDefaults(m harness.CapabilityMatrix, prefs []RolePreference) (defaults []RoleDefault, gaps []harness.Capability) {
	row := m.Capability(harness.RoleModelDefaults)
	if row.Status != harness.StatusSupported {
		if row.Limitation == "" {
			row.Limitation = "no CP0 role-default observation for " + string(m.Harness) + " " + m.Version
		}
		return nil, []harness.Capability{row}
	}
	for _, p := range prefs {
		defaults = append(defaults, RoleDefault{Role: p.Role, Preference: p.Preference})
	}
	return defaults, nil
}

// ProviderID reports whether s names a concrete provider or model. Package
// generation rejects any generated file whose rendered bytes match it, so a
// provider choice cannot enter Atomic's output. The check is shared with every
// adapter, so one pattern decides the sweep for all of them.
func ProviderID(s string) bool { return harness.ProviderID(s) }
