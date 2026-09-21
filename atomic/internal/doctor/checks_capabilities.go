package doctor

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// checkCapabilities implements category 18: each registered harness's CP0
// capability record, reported as promised rows against proven ones. A row
// without a retained observation is unsupported, so the check never reads a
// promise as proof.
func checkCapabilities(opts Opts) Result {
	reg, err := loadRegistry(opts)
	if err != nil {
		return unavailability(err)
	}

	kinds := reg.Kinds()
	if len(kinds) == 0 {
		return Result{Severity: PASS, Detail: "no harness adapters registered"}
	}

	var findings, problems []string
	totalProven := 0
	for _, kind := range kinds {
		adapter, err := reg.Adapter(kind)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: adapter unavailable: %v", kind, err))
			continue
		}
		m := adapter.Capabilities()
		if m.Harness != kind {
			problems = append(problems, fmt.Sprintf("%s adapter reports capability record for %s", kind, m.Harness))
		}
		rows := m.Roles()
		supported, partial, unproven := 0, 0, 0
		for _, row := range rows {
			switch row.Status {
			case harness.StatusSupported:
				supported++
			case harness.StatusPartial:
				partial++
			default:
				unproven++
			}
			findings = append(findings, fmt.Sprintf("%s %s: %s", kind, row.Role, row.Status))
		}
		totalProven += supported
		findings = append(findings, fmt.Sprintf("%s %s (observed %s): %d promised, %d proven, %d partial, %d unproven",
			kind, m.Version, m.Observed, len(rows), supported, partial, unproven))
	}

	detail := fmt.Sprintf("%d harness capability record(s), %d role(s) fully proven", len(kinds), totalProven)
	return resultFor(detail, problems, findings)
}
