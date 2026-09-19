package doctor

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

// hooksInstalledFn is the session-start hook trust probe. Tests swap it to
// exercise the drifted branch without hand-building a half-migrated settings
// file.
var hooksInstalledFn = hooks.IsInstalled

// checkTrust implements category 20: deliberate disablement and untrusted hook
// state. A deliberate override wins by design, so the check reports what is
// registered, what is unproven, and refuses to repair either.
func checkTrust(opts Opts) Result {
	home := installHome(opts)
	if home == "" {
		return unavailability(errNoHome)
	}

	var findings, problems []string

	installed, drifted, err := hooksInstalledFn(home)
	switch {
	case err != nil:
		problems = append(problems, "session-start hook state unreadable: "+err.Error())
	case drifted:
		problems = append(problems, "session-start hook is registered but drifted; run `atomic hooks install`")
		findings = append(findings, fmt.Sprintf("claude session-start hook: installed=%v drifted=%v", installed, drifted))
	default:
		findings = append(findings, fmt.Sprintf("claude session-start hook: installed=%v", installed))
	}

	reg, err := loadRegistry(opts)
	if err != nil {
		findings = append(findings, "harness registry unavailable: "+err.Error())
	} else {
		for _, kind := range reg.Kinds() {
			adapter, err := reg.Adapter(kind)
			if err != nil {
				continue
			}
			row := adapter.Capabilities().Capability(harness.RoleSkillDisablement)
			findings = append(findings, fmt.Sprintf("%s skill-disablement: %s", kind, row.Status))
		}
		// The one deliberate-disablement surface OMP leaves unproven. It is a
		// reported gap, never a fabricated trust claim.
		gap := omp.RuntimeDisablementGap()
		findings = append(findings, fmt.Sprintf("omp %s: %s", gap.Surface, gap.Evidence))
	}

	return resultFor("trust and disablement surfaces reported", problems, findings)
}
