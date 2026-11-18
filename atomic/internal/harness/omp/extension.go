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
// per planned event; extensionEventsPlaceholder names that set in the header.
const (
	extensionHandlersPlaceholder = "/*atomic-handlers*/"
	extensionEventsPlaceholder   = "/*atomic-events*/"
)

// extensionModule is the runtime delivery module the package publishes, in the
// probed shape: a default-exported factory receiving the ExtensionAPI.
const extensionModule = `// Atomic's OMP runtime delivery. Generated from the projected rule corpus by the
// atomic binary; do not edit.

// Registered events: /*atomic-events*/
import { execFile } from "node:child_process";
import { appendFileSync, realpathSync } from "node:fs";

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
	on(event: "session_switch", handler: (event: unknown, ctx: SessionContext) => unknown): void;
	on(event: "before_agent_start", handler: (event: AgentStartEvent, ctx: SessionContext) => unknown): void;
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

// sameDirectory compares resolved paths: OMP reports a resolved cwd where
// session_start may have reported a symlinked one. An unresolvable path is not equal.
function sameDirectory(a: string, b: string): boolean {
	if (a === b) return true;
	try {
		return realpathSync(a) === realpathSync(b);
	} catch {
		return false;
	}
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

// A ".." above the root yields null: the path leaves the base and cannot be
// expressed as a base-relative candidate.
function segments(value: string): string[] | null {
	const out: string[] = [];
	for (const segment of value.split("/")) {
		if (segment === "" || segment === ".") continue;
		if (segment === "..") {
			if (out.length === 0) return null;
			out.pop();
			continue;
		}
		out.push(segment);
	}
	return out;
}

// rebase returns the base-relative candidate the canonical matcher takes, or
// null when the path cannot be placed inside the session's base.
function rebase(candidate: string, base: string): string | null {
	if (candidate === "" || base === "") return null;
	const value = candidate.replace(/\\/g, "/");
	if (value.startsWith("~") || /^[A-Za-z]:/.test(value)) return null;
	const baseSegments = segments(base);
	if (baseSegments === null || baseSegments.length === 0) return null;
	const target = segments(value.startsWith("/") ? value : base + "/" + value);
	if (target === null || target.length <= baseSegments.length) return null;
	for (let i = 0; i < baseSegments.length; i++) {
		if (target[i] !== baseSegments[i]) return null;
	}
	return target.slice(baseSegments.length).join("/");
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

// The bound covers only the static index, in UTF-16 units as the plan measures;
// an index over it is withheld, since a truncated index misreports rules.
function sessionBaseline(matched: string[], uncovered: string[]): string | null {
	if (DATA.indexText.length > DATA.bound) return null;
	const lines = [DATA.indexText];
	// Canonical identity order, so the block reads the same way whatever order the
	// session's operations arrived in.
	const matchedRules = [...matched].sort();
	const uncoveredTools = [...uncovered].sort();
	if (matchedRules.length > 0) lines.push("- matched by this session's proven paths: " + matchedRules.join(" "));
	if (uncoveredTools.length > 0) lines.push("- tools with no proven exact path: " + uncoveredTools.join(" "));
	lines.push("</atomic-rules>");
	return lines.join("\n");
}

const CONTEXT_MARKER = "atomic-omp-session-context";
const CONTEXT_TIMEOUT_MS = 5000;

// loadSessionContext resolves to the verb's text, or to "" on any failure: a
// missing binary, a non-zero exit, or the timeout must never break a session.
function loadSessionContext(cwd: string): Promise<string> {
	if (cwd === "") return Promise.resolve("");
	const args = ["hooks", "session-start", "--format", "text", "--harness", "omp", "--cwd", cwd];
	return new Promise((resolve) => {
		const done = (text: string, reason: string) => {
			emit({ kind: "session_context", units: text.length, reason });
			resolve(text);
		};
		try {
			execFile("atomic", args, { cwd, timeout: CONTEXT_TIMEOUT_MS }, (error, stdout) => {
				if (error) done("", String(error.code ?? error.signal ?? error.message));
				else done(String(stdout).trim(), "");
			});
		} catch (error) {
			done("", String(error));
		}
	});
}

// ownsBlock matches only a block this module wrote, never a prompt part that
// mentions the marker in prose.
function ownsBlock(part: string, tag: string, marker: string): boolean {
	const open = part.split("\n", 1)[0];
	return open.startsWith("<" + tag + " ") && open.includes("marker=\"" + marker + "\"");
}

function sessionContextBlock(text: string): string {
	return "<atomic-session-context marker=\"" + CONTEXT_MARKER + "\">\n" + text + "\n</atomic-session-context>";
}

export default function atomic(pi: ExtensionApi): void {
	// OMP runs the factory again for a child session, so each session carries its
	// own state and its own baseline delivery.
	const matched: string[] = [];
	const uncovered: string[] = [];
	// The workspace root OMP reports at session start; until then every operation
	// is uncovered, since no path is provably contained in an unknown base.
	let base = "";
	let sessionContext: Promise<string> | null = null;
	let deliveries = 0;
	let operations = 0;
	let denials = 0;
	function restart(cwd: string): void {
		base = cwd;
		sessionContext = loadSessionContext(cwd);
		matched.length = 0;
		uncovered.length = 0;
		deliveries = 0;
		operations = 0;
		denials = 0;
	}
	emit({ kind: "factory", pid: process.pid });

/*atomic-handlers*/}
`

// handlerSessionStart observes the tool inventory, the delivered index, and the
// workspace root the runtime matcher rebases operation paths onto.
const handlerSessionStart = `	pi.on("session_start", (_event, ctx) => {
		base = text(ctx?.cwd);
		sessionContext ??= loadSessionContext(base);
		const tools = (pi.getAllTools?.() ?? []).map((tool) => text(tool.name)).filter((name) => name !== "");
		const covered = tools.filter((name) => DATA.tools.some((input) => input.tool === name));
		const uncoveredTools = tools.filter((name) => !DATA.tools.some((input) => input.tool === name));
		emit({
			kind: "session_start",
			cwd: base,
			tools,
			covered,
			uncovered: uncoveredTools,
			index: DATA.index.map((entry) => entry.id),
		});
	});
`

// handlerSessionSwitch starts the switched session's state over, since OMP runs
// it in the same extension instance without a new session_start.
const handlerSessionSwitch = `	pi.on("session_switch", (_event, ctx) => {
		restart(text(ctx?.cwd));
	});
`

// handlerBeforeAgentStart supplies the bounded session-baseline block and the
// session context the session_start handler loaded. OMP's /move fires no event,
// so a ctx.cwd that differs from the loaded directory restarts the session state.
const handlerBeforeAgentStart = `	pi.on("before_agent_start", async (event, ctx) => {
		const cwd = text(ctx?.cwd);
		if (base !== "" && cwd !== "" && !sameDirectory(cwd, base)) restart(cwd);
		const prompt = strings(event?.systemPrompt);
		const context = (await sessionContext) ?? "";
		const added: string[] = [];
		const block = sessionBaseline(matched, uncovered);
		if (block === null) {
			emit({ kind: "baseline_suppressed", units: DATA.indexText.length, bound: DATA.bound });
		} else {
			deliveries += 1;
			emit({ kind: "baseline", deliveries, bytes: block.length, bound: DATA.bound, replaced: prompt.filter((part) => ownsBlock(part, "atomic-rules", DATA.marker)).length, matched });
			added.push(block);
		}
		if (context !== "") added.push(sessionContextBlock(context));
		if (added.length === 0) return;
		// A block already in the array is replaced: before_agent_start runs every
		// turn, and the session keeps the block only while each turn re-supplies it.
		const kept = prompt.filter((part) => !ownsBlock(part, "atomic-rules", DATA.marker) && !ownsBlock(part, "atomic-session-context", CONTEXT_MARKER));
		return { systemPrompt: [...kept, ...added] };
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
		const candidate = rebase(text(field(input, proven.path_field)), base);
		if (candidate === null) {
			emit({ kind: "uncovered_operation", tool: name, candidate: text(field(input, proven.path_field)), reason: "the proven field carried no path contained in the session base" });
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

// renderHandlers composes a handler per planned event; the tool_call handler
// carries only the branches whose roles are planned.
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
	if wired["session_switch"] {
		b.WriteString(handlerSessionSwitch)
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

// RenderExtension renders the runtime delivery module the package publishes,
// deterministically and offline.
func (d SessionDelivery) RenderExtension() ([]byte, error) {
	// A nil slice would marshal as null, which the module would fail on at
	// runtime instead of reading an empty index.
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

// RegisteredEvents names the events the rendered module registers. It reads the
// bytes, not the plan, so an audit catches a template registering an unplanned event.
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
