package omp

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/artifacts"
	"github.com/damusix/atomic-claude/atomic/internal/harness"
	"github.com/damusix/atomic-claude/atomic/internal/managedfile"
	"github.com/damusix/atomic-claude/atomic/internal/rules"
)

// OMP proved no context-return result for the current operation, so a match is
// reported by the next session baseline, never before the operation itself.

// BaselineByteBound is Atomic's own ceiling on the static rule index, in UTF-16
// code units (`String.length`); an index over it is suppressed, never truncated.
const BaselineByteBound = 8192

// RuntimeMarker identifies the Atomic block inside a system prompt, so the next
// `before_agent_start` turn replaces the previous copy instead of stacking one.
const RuntimeMarker = "atomic-omp-runtime"

// RuntimeLogEnv points the generated module at its observation log. Unset
// disables observation, never delivery.
const RuntimeLogEnv = "ATOMIC_OMP_RUNTIME_LOG"

// ProvenToolInput is one tool whose input carried an exact path in a runtime
// observation. Any other tool is uncovered, and shell strings are never parsed.
type ProvenToolInput struct {
	Tool      string `json:"tool"`
	PathField string `json:"path_field"`
	Evidence  string `json:"evidence"`
}

// ProvenToolInputs returns the structured inputs proven for OMP 18.1.18. Edit,
// write, and apply-patch schemas were never observed, so they stay uncovered.
func ProvenToolInputs() []ProvenToolInput {
	return []ProvenToolInput{{
		Tool:      "read",
		PathField: "path",
		Evidence:  "omp.pre-operation-event, omp.structured-targets; replay target omp-tools",
	}}
}

// DenyEvidence is the runtime evidence every exact predicate carries: one intercepted
// `bash` call was blocked and its unique side effect was proved absent.
const DenyEvidence = "omp.deny, omp.role.deterministic-deny; replay target run-omp-deny-probe.sh"

// DenyPredicate is one exact machine predicate the runtime blocks: full-string
// equality on a `bash` call's `command`, the only form a probe proved.
type DenyPredicate struct {
	Tool     string `json:"tool"`
	Command  string `json:"command"`
	Reason   string `json:"reason"`
	Evidence string `json:"evidence"`
}

// validate refuses a predicate whose form no probe exercised.
func (p DenyPredicate) validate() error {
	if p.Tool != "bash" {
		return fmt.Errorf("omp: deny predicate for tool %q: only the exercised bash predicate form is available", p.Tool)
	}
	if p.Command == "" {
		return fmt.Errorf("omp: deny predicate for bash: an exact command string is required")
	}
	if p.Reason == "" {
		return fmt.Errorf("omp: deny predicate for bash %q: a reason is required so the blocked call explains itself", p.Command)
	}
	return nil
}

// IndexPattern is one scope pattern in the runtime index. Regex comes from
// rules.CompileGlob so the runtime and canonical matchers agree by construction.
type IndexPattern struct {
	Glob  string `json:"glob"`
	Regex string `json:"regex"`
}

// RuleIndexEntry is one rule the session baseline reports.
type RuleIndexEntry struct {
	RecordID string      `json:"id"`
	Class    rules.Class `json:"class"`
	// Source is the rule's path inside Atomic's OMP package tree, which is where
	// the session-baseline block tells the model the body lives.
	Source       string                    `json:"source"`
	SourceDigest string                    `json:"digest"`
	Patterns     []IndexPattern            `json:"patterns"`
	Tier         artifacts.EnforcementTier `json:"tier"`
	Order        int                       `json:"order"`
}

// UndeliverableRule is one projected rule whose patterns leave the translated
// dialect; an approximated pattern would match paths the canonical matcher does not.
type UndeliverableRule struct {
	RecordID string `json:"record_id"`
	Reason   string `json:"reason"`
}

// EventWiring is one event the generated module registers. An event whose
// justifying role is unproven is absent, not registered defensively.
type EventWiring struct {
	Event    string                   `json:"event"`
	Role     harness.Role             `json:"role,omitempty"`
	Status   harness.CapabilityStatus `json:"status"`
	Delivery string                   `json:"delivery"`
	Evidence string                   `json:"evidence"`
	// Observation marks an event that only records; it claims no behavior, so it
	// is wired whenever the module runs.
	Observation bool `json:"observation,omitempty"`
}

// DedupeContract records the observed duplication behavior, so the runtime
// delivery cannot quietly change it.
type DedupeContract struct {
	// Factory is the native extension-registration behavior.
	Factory string `json:"factory"`
	// Injection is Atomic's own rule: one Atomic block per returned system prompt.
	Injection string `json:"injection"`
	// Resume is the observed new-process behavior.
	Resume string `json:"resume"`
	// Child is the observed child-session behavior.
	Child string `json:"child"`
	// Lifetime is how the session keeps the block across turns.
	Lifetime string `json:"lifetime"`
}

// SessionDelivery is the complete runtime delivery plan for OMP.
type SessionDelivery struct {
	Target    artifacts.Target `json:"target"`
	Version   string           `json:"version"`
	Events    []EventWiring    `json:"events"`
	Index     []RuleIndexEntry `json:"index"`
	IndexText string           `json:"-"`
	// IndexUnits is the static index's size in UTF-16 code units, the unit the
	// module measures; a byte count would disagree with every runtime decision.
	IndexUnits int `json:"index_units"`
	Bound      int `json:"bound"`
	// Suppressed reports that the static index alone exceeds the bound, so no
	// session receives a baseline block; the delivery still ships.
	Suppressed    bool                 `json:"suppressed,omitempty"`
	Undeliverable []UndeliverableRule  `json:"undeliverable,omitempty"`
	Tools         []ProvenToolInput    `json:"tools"`
	Deny          []DenyPredicate      `json:"deny,omitempty"`
	Dedupe        DedupeContract       `json:"dedupe"`
	Gaps          []harness.Capability `json:"gaps,omitempty"`
	Unproven      []PackageGap         `json:"unproven,omitempty"`
}

// RuntimeDisablementGap is the unobserved `--no-extensions` surface, kept here
// so doctor has one owner for it.
func RuntimeDisablementGap() PackageGap {
	return PackageGap{
		Surface:  "extension disablement",
		Evidence: "omp.disablement covers --no-rules only; --no-extensions was never exercised, so a disabled extension is unreported rather than observed",
	}
}

// BuildSessionDelivery plans OMP's runtime rule delivery for one projected rule
// set, offline and deterministically.
func BuildSessionDelivery(sources []harness.RuleSource, m harness.CapabilityMatrix, deny []DenyPredicate) (SessionDelivery, error) {
	if m.Harness != harness.KindOMP {
		return SessionDelivery{}, fmt.Errorf("omp: session delivery needs the OMP capability record, got %q", m.Harness)
	}
	records := make([]rules.RuleRecord, 0, len(sources))
	for i, s := range sources {
		if s.Record.ID == "" {
			return SessionDelivery{}, fmt.Errorf("omp: session delivery: rule source %d has no record identity", i)
		}
		if got := managedfile.Digest(s.Bytes); got != s.Record.SourceDigest {
			return SessionDelivery{}, fmt.Errorf("omp: session delivery: %s source bytes digest %s does not match record digest %s", s.Record.ID, got, s.Record.SourceDigest)
		}
		records = append(records, s.Record)
	}
	if err := rules.Validate(records); err != nil {
		return SessionDelivery{}, err
	}

	ordered := make([]rules.RuleRecord, len(records))
	copy(ordered, records)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })

	delivery := SessionDelivery{
		Target:  artifacts.TargetOMP,
		Version: m.Version,
		Events:  sessionEvents(m),
		Tools:   ProvenToolInputs(),
		Dedupe:  sessionDedupe(),
		Gaps:    harness.RuleGaps(m),
		// Wiki cards are bound to one checkout and would make the shared package
		// per-repository, so the runtime index carries the shipped corpus only.
		Unproven: []PackageGap{{
			Surface:  "repository wiki card runtime index",
			Evidence: "omp.static-scope-matching: a paths-scoped project rule never reached the model; the shared package carries no per-repository card index",
		}, {
			Surface:  "pre-operation matched-body delivery",
			Evidence: "omp.context-return-absence; replay target omp-scope (absence)",
		}, RuntimeDisablementGap()},
		Bound: BaselineByteBound,
	}
	ev := harness.RuleEvidence(m)
	for i, record := range ordered {
		entry := RuleIndexEntry{
			RecordID:     record.ID,
			Class:        record.Class,
			Source:       record.Source,
			SourceDigest: record.SourceDigest,
			Tier:         rules.Select(record, artifacts.TargetOMP, ev).Tier,
			Order:        i,
		}
		untranslatable := ""
		for _, glob := range record.Include {
			compiled, err := rules.CompileGlob(glob)
			if err != nil {
				untranslatable = err.Error()
				break
			}
			entry.Patterns = append(entry.Patterns, IndexPattern{Glob: glob, Regex: compiled})
		}
		if untranslatable != "" {
			delivery.Undeliverable = append(delivery.Undeliverable, UndeliverableRule{RecordID: record.ID, Reason: untranslatable})
			continue
		}
		delivery.Index = append(delivery.Index, entry)
	}

	for _, predicate := range deny {
		if err := predicate.validate(); err != nil {
			return SessionDelivery{}, err
		}
		carried := predicate
		if carried.Evidence == "" {
			carried.Evidence = DenyEvidence
		}
		delivery.Deny = append(delivery.Deny, carried)
	}

	delivery.IndexText = baselineIndexText(delivery.Index)
	delivery.IndexUnits = utf16Units(delivery.IndexText)
	delivery.Suppressed = delivery.IndexUnits > delivery.Bound
	return delivery, nil
}

// utf16Units is the `String.length` the module reports for s. Planning must use
// the same unit, or a non-ASCII index plans deliverable while every session suppresses it.
func utf16Units(s string) int {
	units := 0
	for _, r := range s {
		if r > 0xFFFF {
			units += 2
			continue
		}
		units++
	}
	return units
}

// sessionEvents is the wired event set; a delivering event is included only when
// the role that justifies it is proven.
func sessionEvents(m harness.CapabilityMatrix) []EventWiring {
	events := []EventWiring{
		{
			Event:       "session_start",
			Role:        harness.RoleSessionBaseline,
			Status:      m.Capability(harness.RoleSessionBaseline).Status,
			Delivery:    "observation only",
			Observation: true,
			Evidence:    "omp.role.session-baseline-event; replay target omp-root",
		},
		{
			Event:       "before_provider_request",
			Role:        harness.RoleSessionBaseline,
			Status:      m.Capability(harness.RoleSessionBaseline).Status,
			Delivery:    "observation only: records whether the delivered block reached the provider payload",
			Observation: true,
			Evidence:    "omp.role.session-baseline-event; replay target omp-scope",
		},
		{
			Event:       "session_shutdown",
			Role:        harness.RoleSessionBaseline,
			Status:      m.Capability(harness.RoleSessionBaseline).Status,
			Delivery:    "observation only: writes the session summary",
			Observation: true,
			Evidence:    "omp.cleanup; replay target omp-child",
		},
	}
	if m.Supports(harness.RoleSessionBaseline) {
		events = append(events, EventWiring{
			Event:    "before_agent_start",
			Role:     harness.RoleSessionBaseline,
			Status:   m.Capability(harness.RoleSessionBaseline).Status,
			Delivery: fmt.Sprintf("bounded session-baseline rule index (bound %d UTF-16 code units)", BaselineByteBound),
			Evidence: "omp.session-baseline, omp.role.session-baseline-event; replay target omp-root",
		}, EventWiring{
			Event:    "session_switch",
			Role:     harness.RoleSessionBaseline,
			Status:   m.Capability(harness.RoleSessionBaseline).Status,
			Delivery: "reloads the session context and restarts the matched-rule state for the switched session's directory; before_agent_start does the same when /move changed ctx.cwd",
			Evidence: "omp.role.session-baseline-event; TestOMPMoveAndNewSessionBothReloadContext observed reason new and the /move reload at before_agent_start; TestExtensionModuleReloadsSessionContextWhenBeforeAgentStartReportsANewDirectory; resume and fork read from the OMP source, not observed",
		})
	}
	if m.Supports(harness.RolePreOperationTargets) {
		events = append(events, EventWiring{
			Event:    "tool_call",
			Role:     harness.RolePreOperationTargets,
			Status:   m.Capability(harness.RolePreOperationTargets).Status,
			Delivery: "exact-path matching for proven structured inputs; uncovered tools recorded",
			Evidence: "omp.pre-operation-event, omp.structured-targets; replay target omp-tools",
		})
	}
	if m.Supports(harness.RoleDeterministicDeny) {
		events = append(events, EventWiring{
			Event:    "tool_call",
			Role:     harness.RoleDeterministicDeny,
			Status:   m.Capability(harness.RoleDeterministicDeny).Status,
			Delivery: "exact predicate deny",
			Evidence: DenyEvidence,
		})
	}
	return events
}

// sessionDedupe records the duplication contract, one field per capability row.
func sessionDedupe() DedupeContract {
	return DedupeContract{
		Factory:   "omp.extension-discovery, omp.dedupe: one resolved extension path runs one factory per process, even when it is also named explicitly",
		Injection: "one Atomic block per returned system prompt; a block already present is replaced, never stacked",
		Resume:    "omp.resume: a resumed session starts a new process, so the factory, session_start, and the baseline delivery run again",
		Child:     "omp.child: a child session runs its own factory and session_start in the same process and receives its own baseline delivery, never the parent's",
		Lifetime:  "the block is supplied on every before_agent_start call, which is how the session keeps it",
	}
}

// baselineIndexText renders the static part of the session-baseline block. The
// module bounds this text alone, so the plan's verdict and its decision agree.
func baselineIndexText(index []RuleIndexEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<atomic-rules source=\"omp-runtime\" delivery=\"session-baseline\" marker=%q>\n", RuntimeMarker)
	b.WriteString("Atomic path-scoped rules ship in Atomic's OMP package tree (the packages/omp/atomic\n")
	b.WriteString("directory under the Atomic home, ~/.atomic by default), and each source below is\n")
	b.WriteString("relative to that tree. OMP delivers no rule body automatically, so read the named\n")
	b.WriteString("file there when an operation touches a path one of them scopes.\n")
	for _, entry := range index {
		globs := make([]string, 0, len(entry.Patterns))
		for _, pattern := range entry.Patterns {
			globs = append(globs, pattern.Glob)
		}
		fmt.Fprintf(&b, "- %s source %s digest %s paths %s\n", entry.RecordID, entry.Source, entry.SourceDigest, strings.Join(globs, " "))
	}
	return b.String()
}

// extensionIndexEntry is the module's view of one indexed rule. Class and tier
// are reporting vocabulary, not runtime decisions, so they stay out of it.
type extensionIndexEntry struct {
	RecordID string         `json:"id"`
	Digest   string         `json:"digest"`
	Patterns []IndexPattern `json:"patterns"`
	Order    int            `json:"order"`
}

// extensionData is the JSON payload the generated module carries. It is a
// struct, not a map, so the bytes are deterministic.
type extensionData struct {
	Marker string `json:"marker"`
	// Events is the wired event set, carried so the module is self-describing:
	// RenderExtension registers exactly these and no others.
	Events    []string              `json:"events"`
	Bound     int                   `json:"bound"`
	IndexText string                `json:"indexText"`
	Index     []extensionIndexEntry `json:"index"`
	Tools     []ProvenToolInput     `json:"tools"`
	Deny      []DenyPredicate       `json:"deny"`
}

// RuntimeRecord is one observation line the generated module appends to the
// runtime log. Unknown members are ignored, so newer records stay readable.
type RuntimeRecord struct {
	Kind       string   `json:"kind"`
	PID        int      `json:"pid,omitempty"`
	Cwd        string   `json:"cwd,omitempty"`
	Tools      []string `json:"tools,omitempty"`
	Covered    []string `json:"covered,omitempty"`
	Uncovered  []string `json:"uncovered,omitempty"`
	Index      []string `json:"index,omitempty"`
	Deliveries int      `json:"deliveries,omitempty"`
	Bytes      int      `json:"bytes,omitempty"`
	Bound      int      `json:"bound,omitempty"`
	Replaced   int      `json:"replaced,omitempty"`
	Matched    []string `json:"matched,omitempty"`
	Tool       string   `json:"tool,omitempty"`
	Candidate  string   `json:"candidate,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Operations int      `json:"operations,omitempty"`
	Denials    int      `json:"denials,omitempty"`
	Evidence   string   `json:"evidence,omitempty"`
	// HasBaseline reports whether the provider payload carried the Atomic block.
	HasBaseline bool `json:"hasBaseline,omitempty"`
}

// RuntimeLog is a parsed runtime log plus the facts a reader reports on.
type RuntimeLog struct {
	Path               string          `json:"path"`
	Records            []RuntimeRecord `json:"records,omitempty"`
	Factories          int             `json:"factories"`
	Sessions           int             `json:"sessions"`
	BaselineDeliveries int             `json:"baseline_deliveries"`
	Suppressions       int             `json:"suppressions"`
	Operations         int             `json:"operations"`
	Denials            int             `json:"denials"`
	// Matched names every rule ID the session's evidenced paths matched.
	Matched []string `json:"matched,omitempty"`
	// UncoveredTools names every tool with no proven structured input, so its
	// operations can never be matched.
	UncoveredTools []string `json:"uncovered_tools,omitempty"`
	// UncoveredOperations counts operations on a proven tool whose input carried
	// no usable exact path, so they were recorded instead of matched.
	UncoveredOperations int      `json:"uncovered_operations,omitempty"`
	Index               []string `json:"index,omitempty"`
	// DeclaredUncovered names the tools a session reported without a proven
	// structured input, whether or not any of them was called.
	DeclaredUncovered []string `json:"declared_uncovered,omitempty"`
	// BaselineConsumed reports that a delivered block reached the provider
	// payload, the observation the session-baseline row records.
	BaselineConsumed bool `json:"baseline_consumed"`
}

// ReadRuntimeLog parses a runtime log the generated module wrote. A missing log
// is an empty log, not an error: a session that never ran is not a failure.
func ReadRuntimeLog(path string) (RuntimeLog, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return RuntimeLog{Path: path}, nil
	}
	if err != nil {
		return RuntimeLog{Path: path}, fmt.Errorf("omp: read runtime log: %w", err)
	}
	return ParseRuntimeLog(path, data)
}

// ParseRuntimeLog parses retained runtime records for a caller that already
// holds the bytes.
func ParseRuntimeLog(name string, data []byte) (RuntimeLog, error) {
	log := RuntimeLog{Path: name}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var record RuntimeRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return log, fmt.Errorf("omp: parse runtime log %s: %w", name, err)
		}
		log.Records = append(log.Records, record)
		switch record.Kind {
		case "factory":
			log.Factories++
		case "session_start":
			log.Sessions++
			log.Index = append(log.Index, record.Index...)
			log.DeclaredUncovered = append(log.DeclaredUncovered, record.Uncovered...)
		case "baseline":
			log.BaselineDeliveries++
		case "baseline_suppressed":
			log.Suppressions++
		case "operation":
			log.Operations++
			log.Matched = append(log.Matched, record.Matched...)
		case "uncovered_tool":
			log.UncoveredTools = append(log.UncoveredTools, record.Tool)
		case "uncovered_operation":
			log.UncoveredOperations++
		case "deny":
			log.Denials++
		case "provider_request":
			log.BaselineConsumed = log.BaselineConsumed || record.HasBaseline
		}
	}
	log.Matched = uniqueSorted(log.Matched)
	log.UncoveredTools = uniqueSorted(log.UncoveredTools)
	log.DeclaredUncovered = uniqueSorted(log.DeclaredUncovered)
	log.Index = uniqueSorted(log.Index)
	return log, nil
}

func uniqueSorted(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// RuntimeProof is the retained evidence from the last runtime scenario run:
// observed facts, never the plan's expectations.
type RuntimeProof struct {
	Scenario string   `json:"scenario"`
	Harness  string   `json:"harness"`
	Version  string   `json:"version"`
	Observed string   `json:"observed"`
	Events   []string `json:"events,omitempty"`
	// RulesDisabled reports a deliberate `--no-rules` run: native rule content was
	// absent while the Atomic extension still delivered its baseline.
	RulesDisabled bool        `json:"rules_disabled,omitempty"`
	Log           *RuntimeLog `json:"log,omitempty"`
}

// RuntimeStatus is the read-only runtime delivery status doctor and status
// commands report.
type RuntimeStatus struct {
	Delivery SessionDelivery `json:"delivery"`
	Proof    *RuntimeProof   `json:"proof,omitempty"`
}

// RuntimeStatus reports one OMP target's runtime delivery without writing
// anything. A nil proof is the normal state before any scenario ran.
func (a *Adapter) RuntimeStatus(sources []harness.RuleSource, deny []DenyPredicate, proof *RuntimeProof) (RuntimeStatus, error) {
	delivery, err := BuildSessionDelivery(sources, a.Capabilities(), deny)
	if err != nil {
		return RuntimeStatus{}, err
	}
	return RuntimeStatus{Delivery: delivery, Proof: proof}, nil
}
