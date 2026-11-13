package harness

import "sort"

// CapabilityStatus is how much of a capability runtime evidence proves for the
// harness version. Documentation is never proof; an unobserved row is unsupported.
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

// Role names a native surface role so no adapter assumes a product API name. A
// missing role degrades to unsupported instead of stranding installation.
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
	// RoleModelDefaults carries an adapter-written model default, written only
	// where its precedence over user settings is proven, never as a provider ID.
	RoleModelDefaults Role = "model-defaults"
	// RoleAgentDelivery is the native agent directory the harness lists agents
	// from and runs each with its file's model and effort keys.
	RoleAgentDelivery Role = "agent-delivery"
	// RoleSessionContext is the native hook that runs `atomic hooks
	// session-start` and brings its output into the model's context.
	RoleSessionContext Role = "session-context"
	// RoleCommandDelivery is the native command directory the harness expands
	// `/<name> <args>` from.
	RoleCommandDelivery Role = "command-delivery"
	// RoleSteeringImports is the native expansion of a whole-line `@path`
	// import inside a steering file.
	RoleSteeringImports Role = "steering-imports"
	// RoleOutputStyleSelection is the native surface that selects and revokes
	// an output style.
	RoleOutputStyleSelection Role = "output-style-selection"
	// RoleGlobalSteering is the user-level steering file the harness reads into
	// every session.
	RoleGlobalSteering Role = "global-steering"
	// RoleWikiCardDelivery is the surface that puts a repository wiki card in
	// front of the model for the files it is about to edit.
	RoleWikiCardDelivery Role = "wiki-card-delivery"
)

// Capability is one capability row. Evidence holds the claim IDs and replay
// targets in docs/research/harness-capability-matrix.md.
type Capability struct {
	Role   Role             `json:"role"`
	Status CapabilityStatus `json:"status"`
	// Native names the tested native surface, empty when none was observed.
	Native string `json:"native,omitempty"`
	// Limitation states what the supported or partial portion does not cover.
	Limitation string `json:"limitation,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
	// Version is the harness version this row was observed on, empty when it
	// is the matrix version; the status is proven for that version only.
	Version string `json:"version,omitempty"`
}

// RootSpec records the observed native configuration root.
type RootSpec struct {
	// Env is the environment variable that relocates the native root, empty when
	// none was proven.
	Env string `json:"env,omitempty"`
	// DefaultDir is the native root relative to the OS home.
	DefaultDir string `json:"default_dir,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
}

// CapabilityMatrix is one harness's versioned capability record. A surface is
// first-class only when its row is supported.
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

// Gap returns the row for role and whether it is still unproven.
func (m CapabilityMatrix) Gap(role Role) (Capability, bool) {
	c := m.Capability(role)
	return c, c.Status != StatusSupported
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

// ClaudeCapabilities is the capability record for Claude Code 2.1.273.
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
				Status:     StatusPartial,
				Native:     "rules/",
				Limitation: "Claude Code 2.1.322 loaded an installed paths: rule on an @-mentioned matching file (load_reason path_glob_match) and not at startup; model-issued reads, resumed delivery, and child delivery were never observed",
				Evidence:   "runtimeproof TestClaudeFileReadLoadsNestedWikiLoaderAndScopedRule; superseded absence replay target claude-primary (claude.path-scope-absence)",
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

// OMPCapabilities is the capability record for OMP 18.1.18; a row with its own
// Version was observed on that version instead.
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
				Limitation: "an alwaysApply rule entered the opening system prompt, but a paths-scoped rule never appeared before or after the intended read; on OMP 18.18.3 a globs rule in the profile rules/ is listed by name, globs, and description and its body is absent from the opening request, and a matched-body load after a read needs a model answer the credential-free probe never reaches",
				Evidence:   "omp.static-scope-matching; replay target omp-scope; runtimeproof TestOMPNativeKindDelivery (omp 18.18.3)",
			},
			RoleCommandDelivery: {
				Role:       RoleCommandDelivery,
				Status:     StatusSupported,
				Native:     "<profile agent root>/commands/<name>.md",
				Limitation: "observed for one print-mode `/<name> <args>` prompt; interactive autocomplete and a project .omp/commands copy are unexercised",
				Evidence:   "runtimeproof TestOMPNativeKindDelivery: `/atomic-probe alpha beta` reached the provider request as the body with $ARGUMENTS replaced by `alpha beta`",
				Version:    "18.18.3",
			},
			RoleSkillDiscovery: {
				Role:       RoleSkillDiscovery,
				Status:     StatusSupported,
				Native:     "<profile agent root>/skills/<name>/SKILL.md",
				Limitation: "the name and description are listed in the system prompt when the read tool is enabled and the body is not; the model choosing to load a skill was never observed",
				Evidence:   "runtimeproof TestOMPNativeKindDelivery: the skill name and description reached the provider request",
				Version:    "18.18.3",
			},
			RoleSteeringImports: {
				Role:       RoleSteeringImports,
				Status:     StatusSupported,
				Native:     "whole-line @path in <profile agent root>/AGENTS.md and repository AGENTS.md or CLAUDE.md",
				Limitation: "observed for a ~/ import in the profile AGENTS.md, a relative import in a repository AGENTS.md, and the same import reached only through a repository CLAUDE.md `@AGENTS.md` loader; a missing target stays in the request as its literal @token",
				Evidence:   "runtimeproof TestOMPNativeKindDelivery: each imported file's content reached the provider request",
				Version:    "18.18.3",
			},
			RoleOutputStyleSelection: {
				Role:       RoleOutputStyleSelection,
				Status:     StatusUnsupported,
				Limitation: "OMP documents no output-style surface, so the style ships always on inside the profile AGENTS.md block and can be neither selected nor revoked",
				Evidence:   "the bundled OMP docs carry no output-style setting; no probe target",
				Version:    "18.18.3",
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
			RoleAgentDelivery: {
				Role:       RoleAgentDelivery,
				Status:     StatusUnsupported,
				Native:     "<profile agent root>/agents/<name>.md",
				Limitation: "the opening request's task tool listed the agent from the profile agent root, but no model turn ran, so the agent running with its model and thinking keys was not observed",
				Evidence:   "runtimeproof TestOMPAgentOverrideIsProjectedAndTheAgentListed, observed 2026-11-11 on omp 18.18.3: the override reached the agent file and the agent was listed",
			},
		},
	}
}

// CodexCapabilities is the record for Codex CLI 0.147.0. No hook event was
// reached; the kind rows come from `codex debug prompt-input`, which needs no model.
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
				Limitation: "Codex has no path-scoped rule surface; the plugin's rule bodies reach the model only through a pre-operation hook, and no hook event was reached",
				Evidence:   "codex.hook-absence; replay target codex-runtime",
			},
			RoleGlobalSteering: {
				Role:       RoleGlobalSteering,
				Status:     StatusSupported,
				Native:     "$CODEX_HOME/AGENTS.md",
				Limitation: "observed in the rendered prompt input ahead of the repository AGENTS.md; the model acting on it was never observed",
				Evidence:   "runtimeproof TestCodexNativeKindDelivery, observed 2026-11-11: the file's marker reached `codex debug prompt-input`",
			},
			RoleSkillDiscovery: {
				Role:       RoleSkillDiscovery,
				Status:     StatusSupported,
				Native:     "registered plugin skills/<name>/SKILL.md, listed as atomic:<name>",
				Limitation: "listed only once the plugin is registered; the name, description, and path are listed and the body is not; the model choosing to load a skill was never observed",
				Evidence:   "runtimeproof TestCodexNativeKindDelivery, observed 2026-11-11: the skill name and description reached `codex debug prompt-input`",
			},
			RoleCommandDelivery: {
				Role:       RoleCommandDelivery,
				Status:     StatusUnsupported,
				Limitation: "a registered plugin carries no command surface and `/<name> <args>` reached the prompt input as literal user text; custom prompts are deprecated in favor of skills",
				Evidence:   "runtimeproof TestCodexNativeKindDelivery, observed 2026-11-11",
			},
			RoleSteeringImports: {
				Role:       RoleSteeringImports,
				Status:     StatusUnsupported,
				Limitation: "a whole-line @path in $CODEX_HOME/AGENTS.md or a repository AGENTS.md reached the prompt input as its literal token, never the file's content, so the Atomic block strips its import lines and the profile is not loaded",
				Evidence:   "runtimeproof TestCodexNativeKindDelivery, observed 2026-11-11",
			},
			RoleOutputStyleSelection: {
				Role:       RoleOutputStyleSelection,
				Status:     StatusUnsupported,
				Limitation: "Codex has no output-style setting, so the style ships always on inside the $CODEX_HOME/AGENTS.md block and can be neither selected nor revoked",
				Evidence:   "codex --help and the bundled Codex docs carry no output-style surface, checked 2026-11-11; no probe target",
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
			RoleAgentDelivery: {
				Role:       RoleAgentDelivery,
				Status:     StatusUnsupported,
				Native:     "$CODEX_HOME/agents/<name>.toml",
				Limitation: "the opening request's spawn_agent roles named the agent from $CODEX_HOME/agents with its configured model and effort, but no model turn ran, so a spawned agent running with them was not observed",
				Evidence:   "runtimeproof TestCodexAgentOverrideIsProjectedAndTheRoleListed, observed 2026-11-11 on codex-cli 0.147.0: the override reached the TOML and the role was listed",
			},
			RoleSessionContext: {
				Role:       RoleSessionContext,
				Status:     StatusUnsupported,
				Native:     "$CODEX_HOME/hooks.json SessionStart",
				Limitation: "once trusted, the hook's additionalContext reached the opening request at startup, but no model turn ran, so the model receiving it and resume delivery were not observed",
				Evidence:   "runtimeproof TestCodexSessionStartHookDeliversTheResolvedWorkflowBlockOnceTrusted, observed 2026-11-11 on codex-cli 0.147.0: the trusted hook's context reached the request and the untrusted one's did not",
			},
			RoleWikiCardDelivery: {
				Role:       RoleWikiCardDelivery,
				Status:     StatusPartial,
				Native:     "$CODEX_HOME/hooks.json PreToolUse apply_patch",
				Limitation: "edits only: Codex lists the owned member and Atomic's hash equals Codex's, and the verb emits additionalContext for an apply_patch payload, but no model turn ran, so the model receiving a card, reads, and shell edits were not observed; the member needs /hooks trust review",
				Evidence:   "runtimeproof TestCodexPreToolUseHookIsAcceptedAndDeliversTheMatchingCard, observed 2026-11-12 on codex-cli 0.147.0; runtime/codex/codex-pre-tool-use-cards.json",
			},
		},
	}
}

// cp0Date is the day the retained runtime observations were recorded.
const cp0Date = "2026-09-15"
