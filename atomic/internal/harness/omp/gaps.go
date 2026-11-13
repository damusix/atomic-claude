package omp

import "github.com/damusix/atomic-claude/atomic/internal/harness"

var unprovenKinds = []struct {
	role    harness.Role
	surface string
}{
	{harness.RoleAgentDelivery, "agent delivery"},
	{harness.RoleOutputStyleSelection, "output-style selection"},
}

func kindGaps(m harness.CapabilityMatrix) []PackageGap {
	var out []PackageGap
	for _, k := range unprovenKinds {
		if row, open := m.Gap(k.role); open {
			out = append(out, PackageGap{Surface: k.surface, Evidence: row.Limitation})
		}
	}
	return out
}
