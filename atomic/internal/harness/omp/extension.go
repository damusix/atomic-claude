package omp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/harness"
)

// extensionDataPlaceholder is the empty delivery literal in extensionModule the
// rendered payload replaces, so the template stays valid TypeScript on its own.
const extensionDataPlaceholder = `{ marker: "", events: [], bound: 0, indexText: "", index: [], tools: [], deny: [] }`

// extensionHandlersPlaceholder marks where RenderExtension inserts one handler
// per planned event; extensionEventsPlaceholder names the planned event set in
// the module header. Both are generated, so the module registers exactly the
// delivery's events and never a surface an unproven capability row would have
// had to justify.
const (
	extensionHandlersPlaceholder = "/*atomic-handlers*/"
	extensionEventsPlaceholder   = "/*atomic-events*/"
)

// extensionModule is the runtime delivery module the package publishes. It is
// the CP0-proven module shape — a default-exported factory receiving the
// ExtensionAPI — plus the CP0-selected events and the projected rule index. It
// is generated, so it carries no authored prose: every handler decision is data
// from the delivery payload, and the only logic here is the bounded baseline
// block, exact-path matching, uncovered-tool recording, and the exact predicate
// deny.
const extensionModule = `// Atomic's OMP runtime delivery. Generated from the projected rule corpus by the
// atomic binary; do not edit.
//
// The module registers exactly the events the delivery planned, listed below: an
// event whose justifying role CP0 did not prove is absent, not registered
// defensively. Every handler decision is data from the delivery payload.
//
// Registered events: /*atomic-events*/
import { appendFileSync } from "node:fs";

type IndexPattern = { glob: string; regex: string };
type IndexEntry = { id: string; digest: string; patterns: IndexPattern[]; order: number };
type ProvenInput = { tool: string; path_field: string; evidence: string };
type DenyRule = { tool: string; command: string; reason: string; evidence: string };
type Delivery = {
	marker: string;
	events: string[];
	bound: number;
	indexText: string;
	index: IndexEntry[];
	tools: ProvenInput[];
	deny: DenyRule[];
};

const DATA: Delivery = /*atomic-delivery*/{ marker: "", events: [], bound: 0, indexText: "", index: [], tools: [], deny: [] };

type ToolCallEvent = { toolName?: unknown; input?: unknown };
type AgentStartEvent = { systemPrompt?: unknown };
type ProviderRequestEvent = { payload?: unknown };
type SessionContext = { cwd?: unknown };
type ToolInfo = { name?: unknown };
type ExtensionApi = {
	on(event: "session_start", handler: (event: unknown, ctx: SessionContext) => unknown): void;
	on(event: "before_agent_start", handler: (event: AgentStartEvent) => unknown): void;
	on(event: "before_provider_request", handler: (event: ProviderRequestEvent) => unknown): void;
	on(event: "tool_call", handler: (event: ToolCallEvent) => unknown): void;
	on(event: "session_shutdown", handler: () => unknown): void;
	getAllTools?(): ToolInfo[];
};

function text(value: unknown): string {
	return typeof value === "string" ? value : "";
}

function field(object: unknown, key: string): unknown {
	if (typeof object !== "object" || object === null) return undefined;
	return (object as Record<string, unknown>)[key];
}

function strings(value: unknown): string[] {
	if (!Array.isArray(value)) return [];
	return value.filter((item): item is string => typeof item === "string");
}

function emit(record: Record<string, unknown>): void {
	const path = process.env.ATOMIC_OMP_RUNTIME_LOG;
	if (!path) return;
	try {
		appendFileSync(path, JSON.stringify(record) + "\n");
	} catch {
		// Observation must never break a session.
	}
}

// normalize accepts only an exact workspace-relative path. An absolute path, a
// parent escape, or an empty field yields null, and the operation is recorded as
// uncovered rather than matched against a path it does not have.
function normalize(candidate: string): string | null {
	if (candidate === "") return null;
	const value = candidate.replace(/\\/g, "/");
	if (value.startsWith("/") || value.startsWith("~") || /^[A-Za-z]:/.test(value)) return null;
	const segments: string[] = [];
	for (const segment of value.split("/")) {
		if (segment === "" || segment === ".") continue;
		if (segment === "..") return null;
		segments.push(segment);
	}
	if (segments.length === 0) return null;
	return segments.join("/");
}

function matchedRuleIds(candidate: string): string[] {
	const ids: string[] = [];
	for (const entry of DATA.index) {
		for (const pattern of entry.patterns) {
			if (new RegExp(pattern.regex).test(candidate)) {
				ids.push(entry.id);
				break;
			}
		}
	}
	return ids;
}

// sessionBaseline renders the bounded baseline block. The index is static data
// from the delivery; the matched and uncovered lines report what this session
// observed. A block over the bound is not delivered at all, because a truncated
// rule index would misreport which rules are in force.
function sessionBaseline(matched: string[], uncovered: string[]): string | null {
	// Canonical identity order, so the block reads the same way whatever order the
	// session's operations arrived in.
	const lines = [DATA.indexText];
	const matchedRules = [...matched].sort();
	const uncoveredTools = [...uncovered].sort();
	if (matchedRules.length > 0) lines.push("- matched by this session's proven paths: " + matchedRules.join(" "));
	if (uncoveredTools.length > 0) lines.push("- tools with no proven exact path: " + uncoveredTools.join(" "));
	lines.push("</atomic-rules>");
	const block = lines.join("\n");
	return block.length > DATA.bound ? null : block;
}

export default function atomic(pi: ExtensionApi): void {
	// Per-factory state: one factory instance is one session. OMP runs the factory
	// once per process and again for a child session, so a child or resumed
	// session carries its own state and its own baseline delivery.
	const matched: string[] = [];
	const uncovered: string[] = [];
	let deliveries = 0;
	let operations = 0;
	let denials = 0;
	emit({ kind: "factory", pid: process.pid });

/*atomic-handlers*/}
`

// handlerSessionStart observes the tool inventory and the delivered index.
const handlerSessionStart = `	pi.on("session_start", (_event, ctx) => {
		const tools = (pi.getAllTools?.() ?? []).map((tool) => text(tool.name)).filter((name) => name !== "");
		const covered = tools.filter((name) => DATA.tools.some((input) => input.tool === name));
		const uncoveredTools = tools.filter((name) => !DATA.tools.some((input) => input.tool === name));
		emit({
			kind: "session_start",
			cwd: text(ctx?.cwd),
			tools,
			covered,
			uncovered: uncoveredTools,
			index: DATA.index.map((entry) => entry.id),
		});
	});
`

// handlerBeforeAgentStart supplies the bounded session-baseline block.
const handlerBeforeAgentStart = `	pi.on("before_agent_start", (event) => {
		const prompt = strings(event?.systemPrompt);
		const block = sessionBaseline(matched, uncovered);
		if (block === null) {
			emit({ kind: "baseline_suppressed", bytes: DATA.indexText.length, bound: DATA.bound });
			return;
		}
		// A block already in the array is replaced: before_agent_start runs every
		// turn, and the session keeps the block only while each turn re-supplies it.
		const kept = prompt.filter((part) => !part.includes(DATA.marker));
		deliveries += 1;
		emit({ kind: "baseline", deliveries, bytes: block.length, bound: DATA.bound, replaced: prompt.length - kept.length, matched });
		return { systemPrompt: [...kept, block] };
	});
`

// handlerBeforeProviderRequest records whether the delivered block reached the
// provider payload.
const handlerBeforeProviderRequest = `	pi.on("before_provider_request", (event) => {
		emit({
			kind: "provider_request",
			hasBaseline: JSON.stringify(event?.payload ?? null).includes(DATA.marker),
		});
	});
`

// toolCallDenyBranch is the exact predicate deny, emitted only when the deny
// role is planned.
const toolCallDenyBranch = `		const denied = DATA.deny.find((rule) => rule.tool === name && rule.command === text(field(input, "command")));
		if (denied) {
			denials += 1;
			emit({ kind: "deny", tool: name, reason: denied.reason, evidence: denied.evidence });
			return { block: true, reason: denied.reason };
		}
`

// toolCallMatchBranch matches proven structured inputs and records uncovered
// tools, emitted only when the pre-operation role is planned.
const toolCallMatchBranch = `		const proven = DATA.tools.find((input_) => input_.tool === name);
		if (!proven) {
			if (!uncovered.includes(name)) uncovered.push(name);
			emit({ kind: "uncovered_tool", tool: name, reason: "no CP0-proven structured input path" });
			return;
		}
		const candidate = normalize(text(field(input, proven.path_field)));
		if (candidate === null) {
			emit({ kind: "uncovered_operation", tool: name, reason: "the proven field carried no usable exact path" });
			return;
		}
		operations += 1;
		const ids = matchedRuleIds(candidate);
		for (const id of ids) {
			if (!matched.includes(id)) matched.push(id);
		}
		emit({ kind: "operation", tool: name, candidate, matched: ids, evidence: proven.evidence });
`

// handlerSessionShutdown writes the session summary.
const handlerSessionShutdown = `	pi.on("session_shutdown", () => {
		emit({ kind: "shutdown", deliveries, operations, denials, matched, uncovered });
	});
`

// renderHandlers composes the handler registrations for the planned event set.
// A handler is emitted only when its event is planned; the tool_call handler
// carries only the branches whose roles are planned, so a deny-only delivery
// registers no matching path and a match-only delivery registers no deny.
func renderHandlers(d SessionDelivery) string {
	wired := make(map[string]bool, len(d.Events))
	canMatch := false
	canDeny := false
	for _, event := range d.Events {
		wired[event.Event] = true
		if event.Event != "tool_call" {
			continue
		}
		switch event.Role {
		case harness.RolePreOperationTargets:
			canMatch = true
		case harness.RoleDeterministicDeny:
			canDeny = true
		}
	}
	var b strings.Builder
	if wired["session_start"] {
		b.WriteString(handlerSessionStart)
	}
	if wired["before_agent_start"] {
		b.WriteString(handlerBeforeAgentStart)
	}
	if wired["before_provider_request"] {
		b.WriteString(handlerBeforeProviderRequest)
	}
	if wired["tool_call"] {
		b.WriteString("\tpi.on(\"tool_call\", (event) => {\n")
		b.WriteString("\t\tconst name = text(event?.toolName);\n")
		b.WriteString("\t\tconst input = event?.input;\n")
		if canDeny {
			b.WriteString(toolCallDenyBranch)
		}
		if canMatch {
			b.WriteString(toolCallMatchBranch)
		}
		b.WriteString("\t});\n")
	}
	if wired["session_shutdown"] {
		b.WriteString(handlerSessionShutdown)
	}
	return b.String()
}

// RenderExtension renders the runtime delivery module the package publishes. It
// is deterministic and offline: identical deliveries produce identical bytes,
// and the module registers exactly the delivery's planned events.
func (d SessionDelivery) RenderExtension() ([]byte, error) {
	// The module evaluates these arrays directly, so they are always present: a
	// nil slice would marshal as null and turn an empty rule set into a runtime
	// error instead of an empty index.
	index := make([]extensionIndexEntry, 0, len(d.Index))
	for _, entry := range d.Index {
		index = append(index, extensionIndexEntry{
			RecordID: entry.RecordID,
			Digest:   entry.SourceDigest,
			Patterns: entry.Patterns,
			Order:    entry.Order,
		})
	}
	tools := d.Tools
	if tools == nil {
		tools = []ProvenToolInput{}
	}
	deny := d.Deny
	if deny == nil {
		deny = []DenyPredicate{}
	}
	wired := d.WiredEvents()
	if wired == nil {
		wired = []string{}
	}
	payload, err := json.MarshalIndent(extensionData{
		Marker:    RuntimeMarker,
		Events:    wired,
		Bound:     d.Bound,
		IndexText: d.IndexText,
		Index:     index,
		Tools:     tools,
		Deny:      deny,
	}, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("omp: render extension data: %w", err)
	}
	module := strings.Replace(extensionModule, extensionHandlersPlaceholder, renderHandlers(d), 1)
	module = strings.Replace(module, extensionEventsPlaceholder, strings.Join(wired, ", "), 1)
	return []byte(strings.Replace(module, extensionDataPlaceholder, string(payload), 1)), nil
}

// WiredEvents names the distinct events the delivery plans to register, in plan
// order. An event planned by more than one role appears once.
func (d SessionDelivery) WiredEvents() []string {
	var names []string
	seen := make(map[string]bool, len(d.Events))
	for _, event := range d.Events {
		if seen[event.Event] {
			continue
		}
		seen[event.Event] = true
		names = append(names, event.Event)
	}
	return names
}

// registeredEventPattern matches the event name in each pi.on registration the
// rendered module emits.
var registeredEventPattern = regexp.MustCompile(`pi\.on\("([A-Za-z_]+)"`)

// RegisteredEvents names the events the rendered module actually registers, in
// first-registration order. It reads the artifact bytes rather than the plan, so
// a gate that audits the rendered module against its delivery sees a template
// registering an unplanned event.
func RegisteredEvents(module []byte) []string {
	var names []string
	seen := map[string]bool{}
	for _, match := range registeredEventPattern.FindAllSubmatch(module, -1) {
		name := string(match[1])
		if seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}
