package doctor

import (
	"fmt"
	"sort"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/harness/codex"
	"github.com/damusix/atomic-claude/atomic/internal/install"
)

// codexSurfacesFn gathers the read-only Codex surface report for one CODEX_HOME
// root. Tests swap it so a category test never reads an environment.
var codexSurfacesFn = readCodexSurfaces

// codexSurfaces is the read-only report category 24 renders for one root.
type codexSurfaces struct {
	root        string
	surfaces    []codex.SurfaceState
	disablement codex.PluginGap
	rules       []harness.Capability
	// trust is what Codex records for the owned SessionStart member, read
	// from its config.toml; Atomic never writes it.
	trust         codex.HookTrust
	trustEvidence string
	// cardTrust is the same record for the PreToolUse member that delivers the
	// wiki cards.
	cardTrust    codex.HookTrust
	cardEvidence string
}

// readCodexSurfaces composes the read-only Codex seams; it writes nothing and runs
// no Codex command.
func readCodexSurfaces(home, root string) codexSurfaces {
	adapter := codex.New()
	trust, evidence := codex.ObserveSessionHookTrust(home, root)
	cardTrust, cardEvidence := codex.ObservePreToolUseHookTrust(home, root)
	return codexSurfaces{
		root:          root,
		surfaces:      append(append(adapter.RuntimeState(root), adapter.RegistrationStateRow(root)), adapter.KindState()...),
		disablement:   codex.RuntimeDisablementGap(),
		rules:         harness.RuleGaps(adapter.Capabilities()),
		trust:         trust,
		trustEvidence: evidence,
		cardTrust:     cardTrust,
		cardEvidence:  cardEvidence,
	}
}

// checkCodex implements category 24, reporting unproven Codex surfaces and never
// repairing one: a fabricated trust or coverage claim is worse than a reported gap.
func checkCodex(opts Opts) Result {
	report, err := loadStatus(opts)
	if err != nil {
		return unavailability(err)
	}

	roots := codexRoots(report)
	if len(roots) == 0 {
		return Result{Severity: PASS, Detail: "no Codex home discovered or enrolled"}
	}

	enrolled := map[string]bool{}
	for _, status := range report.Targets {
		if status.Target.Kind == harness.KindCodex {
			enrolled[status.Target.NativeRoot] = true
		}
	}
	var findings, problems []string
	surfaces, uncovered := 0, 0
	for _, root := range roots {
		state := codexSurfacesFn(opts.Home, root)
		findings = append(findings, fmt.Sprintf("codex %s: %d runtime surface(s) reported, %d unproven rule-delivery role(s)",
			root, len(state.surfaces), len(state.rules)))
		unsupported := 0
		for _, row := range state.surfaces {
			surfaces++
			findings = append(findings, fmt.Sprintf("codex %s %s: %s — %s", root, row.Surface, row.Status, row.Evidence))
			if row.Status != harness.StatusSupported {
				unsupported++
				findings = append(findings, fmt.Sprintf("codex %s %s fix: %s", root, row.Surface, codex.Remediation(opts.Home, root, row.Surface)))
			}
		}
		if enrolled[root] && unsupported > 0 {
			problems = append(problems, fmt.Sprintf("codex %s: %d unsupported surface(s)", root, unsupported))
		}
		findings = append(findings, fmt.Sprintf("codex %s %s: %s", root, state.disablement.Surface, state.disablement.Evidence))
		findings = append(findings, fmt.Sprintf("codex %s session-start hook trust: %s — %s", root, state.trust, state.trustEvidence))
		if state.trust == codex.HookTrustModified {
			findings = append(findings, fmt.Sprintf("codex %s session-start hook trust fix: open Codex, run /hooks, and trust the Atomic SessionStart hook again", root))
		}
		findings = append(findings, fmt.Sprintf("codex %s pre-tool-use hook trust: %s — %s", root, state.cardTrust, state.cardEvidence))
		if state.cardTrust == codex.HookTrustModified {
			findings = append(findings, fmt.Sprintf("codex %s pre-tool-use hook trust fix: open Codex, run /hooks, and trust the Atomic PreToolUse hook again", root))
		}
		for _, role := range state.rules {
			uncovered++
			findings = append(findings, fmt.Sprintf("codex %s uncovered %s: %s", root, role.Role, role.Status))
		}
	}

	detail := fmt.Sprintf("%d Codex home(s), %d runtime surface(s), %d uncovered rule role(s)",
		len(roots), surfaces, uncovered)
	return resultFor(detail, problems, findings)
}

// codexRoots collects discovered and enrolled Codex roots, sorted for a byte-stable
// report. An enrolled root stays even when discovery cannot resolve it.
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
