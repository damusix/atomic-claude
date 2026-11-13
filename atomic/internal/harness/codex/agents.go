package codex

import "github.com/damusix/atomic-claude/atomic/internal/harness"

func agentDeliveryGap(m harness.CapabilityMatrix) (PluginGap, bool) {
	row, open := m.Gap(harness.RoleAgentDelivery)
	return PluginGap{Surface: "agent delivery", Evidence: row.Limitation}, open
}
