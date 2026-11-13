package codex

import "github.com/damusix/atomic-claude/atomic/internal/harness"

var unprovenKinds = []struct {
	role    harness.Role
	surface string
}{
	{harness.RoleCommandDelivery, "command delivery"},
	{harness.RoleSteeringImports, "steering @ imports"},
	{harness.RoleOutputStyleSelection, "output-style selection"},
	{harness.RoleStaticScope, "path-scoped rule bodies"},
	{harness.RoleWikiCardDelivery, "repository wiki pointer cards"},
}

func kindGaps(m harness.CapabilityMatrix) []PluginGap {
	var out []PluginGap
	for _, k := range unprovenKinds {
		if row, open := m.Gap(k.role); open {
			out = append(out, PluginGap{Surface: k.surface, Evidence: row.Limitation})
		}
	}
	return out
}
