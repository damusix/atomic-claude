package harness

import "sort"

// CapabilityStatus is how much of a capability the CP0 runtime evidence proves
// for the named harness version. Documentation and accepted configuration
// syntax are candidates, never proof, so a row without a retained observation
// is unsupported.
type CapabilityStatus string

const (
	// StatusSupported means the behavior was observed for the tested version.
	StatusSupported CapabilityStatus = "supported"
	// StatusPartial means only part of the behavior was observed; the row's
	// limitation says which part is missing.
	StatusPartial CapabilityStatus = "partial"
	// StatusUnsupported means the behavior is absent, unsafe to infer, exercised
	// too narrowly, or was never relevant to claim.
	StatusUnsupported CapabilityStatus = "unsupported"
)

// Role names one native surface role an adapter consumes, so no adapter assumes
// a product API name — neither a rule-delivery role nor a skill-projection
// surface. A missing or failed role produces unsupported behavior instead of
// stranding installation.
type Role string

const (
	// RoleStaticScope is the harness's native path-scoped rule surface.
	RoleStaticScope Role = "static-scoped-rule"
	// RoleSessionBaseline is the event that can deliver context before the model
	// chooses an operation.
	RoleSessionBaseline Role = "session-baseline"
	// RolePreOperationTargets is the pre-operation event exposing structured
	// target paths.
	RolePreOperationTargets Role = "pre-operation-targets"
	// RoleContextReturn is the context-return result available before the
	// current operation.
	RoleContextReturn Role = "context-return"
	// RoleDeterministicDeny is the result that blocks one exact machine
	// predicate on an event the harness mediates.
	RoleDeterministicDeny Role = "deterministic-deny"
	// RoleSkillDiscovery is the native surface that discovers a skill manifest
	// and surfaces its name and description as a trigger.
	RoleSkillDiscovery Role = "skill-discovery"
	// RoleSkillMetadata is the native surface that carries skill metadata
	// beyond the portable name/description pair.
	RoleSkillMetadata Role = "skill-metadata"
	// RoleSkillReferences is the native surface that resolves a skill's
	// referenced files beside its manifest.
	RoleSkillReferences Role = "skill-references"
	// RoleSkillDisablement is the native surface that honors a user-disabled
	// skill or referenced file.
	RoleSkillDisablement Role = "skill-disablement"
	// RoleModelDefaults is the native surface that carries an adapter-written
	// model or reasoning default for a semantic role. A role default may be
	// written only where the surface and its precedence against existing user
	// settings are proven, and never with a concrete provider ID.
	RoleModelDefaults Role = "model-defaults"
)

// Capability is one CP0 row: the tested native surface, how far it is proven,
// and the evidence that fixes it. Evidence holds CP0 claim IDs and replay
// targets from docs/research/harness-capability-matrix.md.
type Capability struct {
	Role   Role             `json:"role"`
	Status CapabilityStatus `json:"status"`
	// Native names the tested native surface, empty when CP0 observed none.
	Native string `json:"native,omitempty"`
	// Limitation states what the supported or partial portion does not cover.
	Limitation string `json:"limitation,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
}

// RootSpec records the native configuration root CP0 observed. DefaultDir is
// relative to the OS home and is empty when CP0 did not exercise a default.
type RootSpec struct {
	// Env is the environment variable that relocates the native root, empty when
	// CP0 proved none.
	Env string `json:"env,omitempty"`
	// DefaultDir is the native root relative to the OS home.
	DefaultDir string `json:"default_dir,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
}

// CapabilityMatrix is one harness's versioned capability record. It is evidence,
// not marketing: a surface is first-class only when its milestone claims it and
// the row is supported.
type CapabilityMatrix struct {
	Harness Kind   `json:"harness"`
	Version string `json:"version"`
	// Observed is the date the rows were retained.
	Observed string   `json:"observed"`
	Root     RootSpec `json:"root"`
	rows     map[Role]Capability
}

// Capability returns the row for role. A role with no row is unsupported, so
// absent evidence can never read as support.
func (m CapabilityMatrix) Capability(role Role) Capability {
	if row, ok := m.rows[role]; ok {
		return row
	}
	return Capability{Role: role, Status: StatusUnsupported}
}

// Supports reports whether role is fully proven for this harness version.
func (m CapabilityMatrix) Supports(role Role) bool {
	return m.Capability(role).Status == StatusSupported
}

// Roles returns every row in stable role order.
func (m CapabilityMatrix) Roles() []Capability {
	out := make([]Capability, 0, len(m.rows))
	for role := range m.rows {
		out = append(out, m.rows[role])
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Role < out[j].Role })
	return out
}

// ClaudeCapabilities is the CP0 record for Claude Code 2.1.273.
func ClaudeCapabilities() CapabilityMatrix {
	return CapabilityMatrix{
		Harness:  KindClaude,
		Version:  "2.1.273",
		Observed: cp0Date,
		Root: RootSpec{
			Env:        "CLAUDE_CONFIG_DIR",
			DefaultDir: ".claude",
			Evidence:   "claude.native-global-root; replay target claude-primary",
		},
		rows: map[Role]Capability{
			RoleStaticScope: {
				Role:       RoleStaticScope,
				Status:     StatusUnsupported,
				Native:     "rules/",
				Limitation: "unconditional user and project rules loaded at startup, but the isolated run could not issue a model request, so path matching, lazy delivery, resumed delivery, and child delivery were never observed",
				Evidence:   "claude.path-scope-absence; replay target claude-primary",
			},
			RoleSessionBaseline: {
				Role:       RoleSessionBaseline,
				Status:     StatusPartial,
				Native:     "SessionStart",
				Limitation: "the hook ran for startup and resume and returned additionalContext, but nothing consumed it before the run failed authentication",
				Evidence:   "claude.session-baseline, claude.role.session-baseline-event; replay target claude-primary",
			},
			RolePreOperationTargets: {
				Role:       RolePreOperationTargets,
				Status:     StatusUnsupported,
				Limitation: "PreToolUse was configured but no tool call was reached, so no structured target path was observed",
				Evidence:   "claude.path-scope-absence; replay target claude-primary",
			},
			RoleContextReturn: {
				Role:       RoleContextReturn,
				Status:     StatusUnsupported,
				Limitation: "no PreToolUse return was exercised",
				Evidence:   "claude.path-scope-absence; replay target claude-primary",
			},
			RoleDeterministicDeny: {
				Role:       RoleDeterministicDeny,
				Status:     StatusUnsupported,
				Limitation: "a deny hook was configured but no tool call was reached",
				Evidence:   "claude.path-scope-absence; replay target claude-primary",
			},
		},
	}
}

// OMPCapabilities is the CP0 record for OMP 18.1.18.
func OMPCapabilities() CapabilityMatrix {
	return CapabilityMatrix{
		Harness:  KindOMP,
		Version:  "18.1.18",
		Observed: cp0Date,
		Root: RootSpec{
			DefaultDir: ".omp/agent",
			Evidence:   "omp.native-global-root; replay target omp-config-path",
		},
		rows: map[Role]Capability{
			RoleStaticScope: {
				Role:       RoleStaticScope,
				Status:     StatusUnsupported,
				Native:     "rules/",
				Limitation: "an alwaysApply rule entered the opening system prompt, but a paths-scoped rule never appeared before or after the intended read, so matched-body delivery has no proof",
				Evidence:   "omp.static-scope-matching; replay target omp-scope",
			},
			RoleSessionBaseline: {
				Role:       RoleSessionBaseline,
				Status:     StatusSupported,
				Native:     "before_agent_start",
				Limitation: "numeric limits are unmeasured",
				Evidence:   "omp.role.session-baseline-event; replay target omp-root",
			},
			RolePreOperationTargets: {
				Role:       RolePreOperationTargets,
				Status:     StatusSupported,
				Native:     "tool_call",
				Limitation: "only the exercised built-ins are covered: read.input.path is structured, shell command strings are never target paths, and edit/write/apply-patch schemas are unexercised",
				Evidence:   "omp.pre-operation-event, omp.structured-targets; replay target omp-tools",
			},
			RoleContextReturn: {
				Role:       RoleContextReturn,
				Status:     StatusUnsupported,
				Limitation: "no context-return result was exercised; baseline before_agent_start context is not a pre-operation return",
				Evidence:   "omp.context-return-absence; replay target omp-scope",
			},
			RoleDeterministicDeny: {
				Role:       RoleDeterministicDeny,
				Status:     StatusSupported,
				Native:     "tool_call block result",
				Limitation: "proven only for one exact intercepted bash call; coverage for other commands and tools is unsupported",
				Evidence:   "omp.deny, omp.role.deterministic-deny; replay target run-omp-deny-probe.sh",
			},
			RoleModelDefaults: {
				Role:       RoleModelDefaults,
				Status:     StatusUnsupported,
				Native:     "agent config.yml",
				Limitation: "the replay setup wrote defaultModel into the agent config, but every exercised session selected a different provider and model, so neither honoring that default nor its precedence against an existing user setting was observed; role names were seen only as CLI flags",
				Evidence:   "replay target omp-config-path locates the agent root; the config write and the default's effect have no replay target",
			},
		},
	}
}

// CodexCapabilities is the CP0 record for Codex CLI 0.147.0. The isolated
// runtime failed model validation before any hook event, so every hook surface
// is unsupported and only registration has evidence.
func CodexCapabilities() CapabilityMatrix {
	return CapabilityMatrix{
		Harness:  KindCodex,
		Version:  "0.147.0",
		Observed: cp0Date,
		Root: RootSpec{
			Env:      "CODEX_HOME",
			Evidence: "codex.registration; replay target codex-registration",
		},
		rows: map[Role]Capability{
			RoleStaticScope: {
				Role:       RoleStaticScope,
				Status:     StatusUnsupported,
				Limitation: "no completed request or instruction audit",
				Evidence:   "codex.hook-absence; replay target codex-runtime",
			},
			RoleSessionBaseline: {
				Role:       RoleSessionBaseline,
				Status:     StatusUnsupported,
				Limitation: "thread creation occurred but no hook event ran",
				Evidence:   "codex.hook-absence; replay target codex-runtime",
			},
			RolePreOperationTargets: {
				Role:       RolePreOperationTargets,
				Status:     StatusUnsupported,
				Limitation: "no hook event ran, so the documented PreToolUse payload and local-tool inventory are not runtime proof",
				Evidence:   "codex.hook-absence; replay target codex-runtime",
			},
			RoleContextReturn: {
				Role:       RoleContextReturn,
				Status:     StatusUnsupported,
				Limitation: "the documented additionalContext result was never exercised",
				Evidence:   "codex.hook-absence; replay target codex-runtime",
			},
			RoleDeterministicDeny: {
				Role:       RoleDeterministicDeny,
				Status:     StatusUnsupported,
				Limitation: "the documented permissionDecision deny result was never exercised",
				Evidence:   "codex.hook-absence; replay target codex-runtime",
			},
		},
	}
}

// cp0Date is the day the retained runtime observations were recorded.
const cp0Date = "2026-09-15"
