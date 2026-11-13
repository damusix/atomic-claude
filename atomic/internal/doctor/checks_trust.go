package doctor

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/omp"
	"github.com/damusix/atomic-claude/atomic/internal/hooks"
)

// hooksInstalledInDirFn is a seam so tests can reach the drifted branch without
// hand-building a half-migrated settings file.
var hooksInstalledInDirFn = hooks.IsInstalledInDir

// checkTrust implements category 20. A deliberate override wins, so the check
// reports disablement and untrusted hook state and never repairs either.
func checkTrust(opts Opts) Result {
	var findings, problems []string

	scope := claudeScopeFor(opts)
	switch {
	case scope.Skip:
		findings = append(findings, "claude session-start hook: "+claudeNoTargetDetail(nil))
	default:
		for _, root := range scope.Roots {
			installed, drifted, err := hooksInstalledInDirFn(root)
			switch {
			case err != nil:
				problems = append(problems, "session-start hook state unreadable in "+root+": "+err.Error())
			case drifted:
				problems = append(problems, "session-start hook is registered but drifted in "+root+"; run `atomic hooks install`")
				findings = append(findings, fmt.Sprintf("claude session-start hook (%s): installed=%v drifted=%v", root, installed, drifted))
			default:
				findings = append(findings, fmt.Sprintf("claude session-start hook (%s): installed=%v", root, installed))
			}
		}
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
