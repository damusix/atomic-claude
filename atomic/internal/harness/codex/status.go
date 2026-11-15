package codex

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// SurfaceState is one read-only native surface report a doctor check consumes:
// the strongest proven status and the evidence that fixes it.
type SurfaceState struct {
	Surface  string                   `json:"surface"`
	Status   harness.CapabilityStatus `json:"status"`
	Evidence string                   `json:"evidence"`
}

// RuntimeState reports every native runtime surface for one home, read-only. It
// blocks nothing: a missing row must not strand package installation.
func (a *Adapter) RuntimeState(root string) []SurfaceState {
	var rows []SurfaceState
	rows = append(rows, a.TrustState(root)...)
	rows = append(rows, a.CoverageState()...)
	rows = append(rows, a.PayloadState()...)
	rows = append(rows, a.ChildState()...)
	rows = append(rows, a.LastProofState()...)
	rows = append(rows, a.SessionContextState())
	return rows
}

// KindState reports each artifact kind Codex does not receive.
func (a *Adapter) KindState() []SurfaceState {
	var rows []SurfaceState
	for _, gap := range kindGaps(a.Capabilities()) {
		rows = append(rows, SurfaceState{Surface: gap.Surface, Status: harness.StatusUnsupported, Evidence: gap.Evidence})
	}
	return rows
}

// Remediation is the fix doctor prints for an unsupported surface; a surface
// no Atomic action can deliver points at the matrix that tracks its probe.
func Remediation(home, root, surface string) string {
	switch surface {
	case "native marketplace and plugin registration", registrationSurface:
		return registrationRemediation(home, root)
	case "plugin-hook disablement", "deliberate hook disablement and config precedence":
		return "none: honored as Codex records it; " + reenableStep(root)
	}
	return "none in Atomic: unproven on codex-cli " + harness.CodexCapabilities().Version + "; tracked in docs/research/harness-capability-matrix.md"
}

// reenableStep is the fix codex-cli 0.147.0 was observed to honor: `plugin add`
// on a disabled plugin sets `enabled = true`.
func reenableStep(root string) string {
	return fmt.Sprintf("to use it, re-enable with CODEX_HOME=%s `codex plugin add %s`", root, PluginID())
}

func registrationRemediation(home, root string) string {
	state, err := ObserveRegistration(root)
	if err != nil {
		return fmt.Sprintf("repair %s (%v), then run `atomic install --harness codex`", ConfigPath(root), err)
	}
	if state.Plugin && !state.Enabled {
		return fmt.Sprintf("Codex records %s as disabled; %s", PluginID(), reenableStep(root))
	}
	if removed, err := removedByUser(home, harness.Target{Kind: harness.KindCodex, Instance: root, NativeRoot: root}); err == nil && removed {
		return "the plugin was removed from Codex; run `atomic install --harness codex --replace` to re-register"
	}
	if _, err := codexBinary(); err != nil {
		return fmt.Sprintf("put codex-cli on PATH or set %s, then run `atomic install --harness codex`", BinaryEnv)
	}
	return "run `atomic install --harness codex`"
}

// SessionContextState reports whether the SessionStart member's context is
// proven to reach the model, independent of the trust Codex records.
func (a *Adapter) SessionContextState() SurfaceState {
	row := a.Capabilities().Capability(harness.RoleSessionContext)
	return SurfaceState{Surface: "session-context delivery", Status: row.Status, Evidence: row.Limitation}
}

// TrustState reports plugin-hook trust and deliberate disablement, neither of
// which was observed; trust is never inferred from the installer's intent.
func (a *Adapter) TrustState(root string) []SurfaceState {
	return []SurfaceState{
		{
			Surface:  "plugin-hook trust and changed-definition re-review",
			Status:   harness.StatusUnsupported,
			Evidence: "codex.hook-absence; replay target codex-runtime (absence): no hook review or execution occurred",
		},
		{
			Surface:  "deliberate hook disablement and config precedence",
			Status:   harness.StatusUnsupported,
			Evidence: "codex.hook-absence: not exercised, no replay target",
		},
	}
}

// CoverageState reports tool coverage and Atomic's own payload bound, which is
// not a native limit.
func (a *Adapter) CoverageState() []SurfaceState {
	return []SurfaceState{
		{
			Surface:  "mediated tool coverage",
			Status:   harness.StatusUnsupported,
			Evidence: "codex.hook-absence; replay target codex-runtime (absence): the documented PreToolUse payload and local-tool inventory are not runtime proof, so no structured input is proven",
		},
		{
			Surface:  "hosted-tool and specialized-tool bypasses",
			Status:   harness.StatusUnsupported,
			Evidence: "codex.hook-absence: not exercised, no replay target",
		},
		{
			Surface:  "instruction and payload limits",
			Status:   harness.StatusUnsupported,
			Evidence: fmt.Sprintf("no boundary test; Atomic applies its own %d-byte preflight bound to the rule index and refuses an over-limit generation rather than truncating it, and that bound is not a native limit", InstructionByteBound),
		},
	}
}

// PayloadState reports the additional-context spill surface; the documented
// threshold was never exercised, so no spill consumer is proven.
func (a *Adapter) PayloadState() []SurfaceState {
	return []SurfaceState{{
		Surface:  "additional-context spill and consumer resolution",
		Status:   harness.StatusUnsupported,
		Evidence: "codex.hook-absence; replay target codex-runtime (absence): the documented 2,500-token threshold was not boundary-tested",
	}}
}

// ChildState reports child-session behavior. No subagent event was exercised, so
// no child delivery claim exists.
func (a *Adapter) ChildState() []SurfaceState {
	return []SurfaceState{{
		Surface:  "child-session rule delivery",
		Status:   harness.StatusUnsupported,
		Evidence: "codex.hook-absence: SubagentStart and child context were not exercised, no replay target",
	}}
}

// LastProofState reports the last successful runtime proof. None is retained, and
// a missing proof must read as unproven, never as support.
func (a *Adapter) LastProofState() []SurfaceState {
	return []SurfaceState{{
		Surface:  "last successful runtime proof",
		Status:   harness.StatusUnsupported,
		Evidence: "codex.hook-absence; replay target codex-runtime (absence): the isolated runtime never completed a hook observation, so no proof was retained",
	}}
}

// RegistrationStateRow reports the observed native registry state as one surface
// row.
func (a *Adapter) RegistrationStateRow(root string) SurfaceState {
	state, err := ObserveRegistration(root)
	row := SurfaceState{Surface: "native marketplace and plugin registration"}
	switch {
	case err != nil:
		row.Status = harness.StatusUnsupported
		row.Evidence = err.Error()
	case state.Marketplace && state.Plugin && state.Enabled && state.CachePresent:
		row.Status = harness.StatusSupported
		row.Evidence = fmt.Sprintf("codex.registration; replay target codex-registration: %s records the marketplace and an enabled %s plugin", state.ConfigPath, PluginID())
	default:
		row.Status = harness.StatusUnsupported
		row.Evidence = fmt.Sprintf("codex.registration: observed marketplace=%v plugin=%v enabled=%v cache=%v at %s",
			state.Marketplace, state.Plugin, state.Enabled, state.CachePresent, state.ConfigPath)
	}
	return row
}

// RuntimeDisablementGap is the unobserved deliberate-disablement surface, kept
// here so doctor has one owner for it.
func RuntimeDisablementGap() PluginGap {
	return PluginGap{
		Surface:  "plugin-hook disablement",
		Evidence: "codex.hook-absence: deliberate disablement and config precedence were not exercised, so a disabled plugin is unreported rather than observed",
	}
}
