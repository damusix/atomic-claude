package omp

import (
	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// Model policy stays with the user: config.yml precedence was never probed, so
// this adapter reports the gap and writes no role default.

// ModelRole names one semantic OMP model role, taken from the CLI's per-role
// model flags; it is not a provider or model identifier.
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

// RolePreference is one semantic execution preference. It carries no provider or
// model ID, so a caller cannot pass a concrete choice through the adapter.
type RolePreference struct {
	Role       ModelRole `json:"role"`
	Preference string    `json:"preference"`
}

// RoleDefault is one resolved role default. It has no value field because the
// adapter never produces a provider or model identifier.
type RoleDefault struct {
	Role       ModelRole `json:"role"`
	Preference string    `json:"preference"`
}

// RoleDefaults resolves semantic preferences onto OMP roles. Until the
// role-default surface is proven it reports the gap and returns no default.
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

// ProviderID reports whether s names a concrete provider or model; package
// generation rejects any file that matches.
func ProviderID(s string) bool { return harness.ProviderID(s) }
