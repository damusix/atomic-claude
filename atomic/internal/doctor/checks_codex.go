package doctor

import (
	"fmt"
	"sort"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
	"github.com/damusix/atomic-claude/atomic/internal/install"
)

// codexSurfacesFn gathers the CP5 read-only Codex surface report for one
// CODEX_HOME root: every native runtime surface, the observed native
// registration row, the one deliberate-disablement gap, and the rule-delivery
// roles the capability record leaves unproven. It writes nothing and runs no
// Codex command. Tests swap it so a category test never reads an environment.
var codexSurfacesFn = readCodexSurfaces

// codexSurfaces is the read-only report category 24 renders for one root.
type codexSurfaces struct {
	root        string
	surfaces    []codex.SurfaceState
	disablement codex.PluginGap
	rules       []harness.Capability
}

// readCodexSurfaces composes the CP5 seams. None of them mutates: the runtime
// surfaces are the CP0-unproven answers, and registration is read from Codex's
// own registry file.
func readCodexSurfaces(root string) codexSurfaces {
	adapter := codex.New()
	return codexSurfaces{
		root:        root,
		surfaces:    append(adapter.RuntimeState(root), adapter.RegistrationStateRow(root)),
		disablement: codex.RuntimeDisablementGap(),
		rules:       harness.RuleGaps(adapter.Capabilities()),
	}
}

// checkCodex implements category 24: the Codex native surfaces CP0 could not
// prove, reported read-only for every discovered or enrolled Codex home. Trust,
// deliberate disablement, mediated coverage, payload spill, child delivery, and
// the last runtime proof are all unproven for the tested version, so each is
// reported with its evidence rather than presented as parity, and no surface is
// ever repaired: a fabricated trust or coverage claim is worse than a reported
// gap.
func checkCodex(opts Opts) Result {
	report, err := loadStatus(opts)
	if err != nil {
		return unavailability(err)
	}

	roots := codexRoots(report)
	if len(roots) == 0 {
		return Result{Severity: PASS, Detail: "no Codex home discovered or enrolled"}
	}

	var findings []string
	surfaces, uncovered := 0, 0
	for _, root := range roots {
		state := codexSurfacesFn(root)
		findings = append(findings, fmt.Sprintf("codex %s: %d runtime surface(s) reported, %d unproven rule-delivery role(s)",
			root, len(state.surfaces), len(state.rules)))
		for _, row := range state.surfaces {
			surfaces++
			findings = append(findings, fmt.Sprintf("codex %s %s: %s — %s", root, row.Surface, row.Status, row.Evidence))
		}
		findings = append(findings, fmt.Sprintf("codex %s %s: %s", root, state.disablement.Surface, state.disablement.Evidence))
		for _, role := range state.rules {
			uncovered++
			findings = append(findings, fmt.Sprintf("codex %s uncovered %s: %s", root, role.Role, role.Status))
		}
	}

	detail := fmt.Sprintf("%d Codex home(s), %d runtime surface(s), %d uncovered rule role(s)",
		len(roots), surfaces, uncovered)
	return resultFor(detail, nil, findings)
}

// codexRoots collects the Codex native roots discovery reported and the ledger
// enrolls, deduplicated and sorted so a report is byte-stable. An enrolled
// target stays reported when discovery cannot resolve the root, because the
// ledger is the fact that the home was enrolled.
func codexRoots(report install.StatusReport) []string {
	seen := map[string]bool{}
	var roots []string
	add := func(root string) {
		if root == "" || seen[root] {
			return
		}
		seen[root] = true
		roots = append(roots, root)
	}
	for _, row := range report.Instances {
		if row.Instance.Kind == harness.KindCodex {
			add(row.Instance.NativeRoot)
		}
	}
	for _, status := range report.Targets {
		if status.Target.Kind == harness.KindCodex {
			add(status.Target.NativeRoot)
		}
	}
	sort.Strings(roots)
	return roots
}
