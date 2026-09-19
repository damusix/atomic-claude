package codex

import (
	"fmt"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// SurfaceState is one read-only native surface report. It is the shape a doctor
// check consumes: the surface, the strongest status CP0 proves for it, and the
// observation or evidence that fixes that status. It is never a mutation and
// never a repair plan.
type SurfaceState struct {
	Surface  string                   `json:"surface"`
	Status   harness.CapabilityStatus `json:"status"`
	Evidence string                   `json:"evidence"`
}

// RuntimeState reports every native runtime surface for one home, read-only.
// Codex 0.147.0 proved only registration and list visibility, so trust,
// deliberate disablement, payload limits, child behavior, tool coverage, and any
// last runtime proof are all unsupported; registration is reported as it is
// observed, never inferred. The report writes nothing and blocks nothing: a
// missing row here must not strand package installation.
func (a *Adapter) RuntimeState(root string) []SurfaceState {
	var rows []SurfaceState
	rows = append(rows, a.TrustState(root)...)
	rows = append(rows, a.CoverageState()...)
	rows = append(rows, a.PayloadState()...)
	rows = append(rows, a.ChildState()...)
	rows = append(rows, a.LastProofState()...)
	return rows
}

// TrustState reports plugin-hook trust and deliberate disablement. Neither was
// observed: the isolated runtime reached thread creation and the account
// rejected the model before any hook event, and `--no-*` disablement was never
// exercised. A trust claim is never fabricated from the installer's intent.
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

// CoverageState reports tool coverage and the payload bound. No tool input is
// proven, so the matcher matches nothing and every operation is uncovered. The
// payload row names Atomic's own preflight bound and says explicitly that it is
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

// PayloadState reports the additional-context spill surface. CP0 never exercised
// the documented threshold, so no spill consumer is proven and no matched body
// is claimed to arrive.
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

// LastProofState reports the last successful runtime proof. None is retained for
// 0.147.0, so the row is unsupported rather than absent: a stale or missing proof
// must read as unproven, never as support.
func (a *Adapter) LastProofState() []SurfaceState {
	return []SurfaceState{{
		Surface:  "last successful runtime proof",
		Status:   harness.StatusUnsupported,
		Evidence: "codex.hook-absence; replay target codex-runtime (absence): the isolated runtime never completed a hook observation, so no proof was retained",
	}}
}

// RegistrationStateRow reports the observed native registry state as one surface
// row. It is supported only when Codex's own registry records the marketplace
// and an enabled plugin entry; every other combination is reported unsupported
// with what was actually observed.
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

// RuntimeDisablementGap is the deliberate-disablement surface CP0 could not
// observe, so doctor has one owner for it. Plugin-hook disablement was never
// exercised for 0.147.0, so a disabled plugin is reported unproven rather than
// assumed honored.
func RuntimeDisablementGap() PluginGap {
	return PluginGap{
		Surface:  "plugin-hook disablement",
		Evidence: "codex.hook-absence: deliberate disablement and config precedence were not exercised, so a disabled plugin is unreported rather than observed",
	}
}
