package codex

import (
	"encoding/json"
	"fmt"
	"strings"
)

// matcherTemplate is the structured-path matcher module the plugin publishes.
// It is generated from the projected index and the proven-input table, so it
// carries no authored prose and registers no hook: the module is a pure data
// and matching surface that a later checkpoint wires only after CP0 proves an
// event. Every decision is data-driven, and the only logic is exact-path
// rebasing onto the observed base, canonical-identity matching, and
// uncovered-operation reporting.
//
// Shell command strings are never parsed for paths: only a field named by the
// proven-input table is read, and that table is empty for Codex 0.147.0.
const matcherTemplate = `// Atomic's Codex rule matcher. Generated from the projected rule corpus by the
// atomic binary; do not edit.
//
// This module registers no hook. Codex 0.147.0 proved no session-baseline,
// pre-operation, context-return, or deny surface, so the plugin manifest wires
// nothing and the matcher is shipped as a pure data surface for a later
// checkpoint. A tool whose structured input CP0 never exercised carries no safe
// path, so every operation this module observes is returned uncovered and
// receives no matched rule body. A shell command string is never parsed.

const INDEX = %s;
const PROVEN_INPUTS = %s;

function text(value) {
	return typeof value === "string" ? value : "";
}

function field(object, key) {
	if (typeof object !== "object" || object === null) return undefined;
	return object[key];
}

// segments collapses "." and empty components and resolves ".." against what
// precedes it. A ".." above the root yields null: the path leaves the base and
// cannot be expressed as a base-relative candidate.
function segments(value) {
	const out = [];
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

// rebase turns one operation path into the exact base-relative candidate the
// canonical matcher takes, or null when the path cannot be placed inside the
// base: a home-relative or Windows path, a parent escape above the base, an
// absolute path outside it, or no observed base at all. The base is the
// workspace root the session observed, so a relative path resolves against it
// and an absolute path must already sit inside it. A path that is not contained
// is reported uncovered rather than matched against a path it does not have.
export function rebase(candidate, base) {
	if (typeof candidate !== "string" || candidate === "" || typeof base !== "string" || base === "") return null;
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

// matchRelative returns every rule identity whose compiled pattern matches one
// exact base-relative path, in the index's canonical identity order.
function matchRelative(path) {
	const ids = [];
	for (const entry of INDEX.rules) {
		for (const pattern of entry.patterns) {
			if (new RegExp(pattern.regex).test(path)) {
				ids.push(entry.id);
				break;
			}
		}
	}
	return ids;
}

// matchPath returns every rule identity whose compiled pattern matches an
// operation path rebased onto the observed base, in the index's canonical
// identity order. A path the base does not contain matches nothing.
export function matchPath(candidate, base) {
	const path = rebase(candidate, base);
	if (path === null) return [];
	return matchRelative(path);
}

// matchOperation reports whether one operation carries a proven exact target
// path. A tool absent from the proven-input table is uncovered, so it receives
// no matched rule body; a path the operation's observed base does not contain
// is uncovered for the same reason. Nothing here blocks or rewrites an
// operation: no deny result is proven for Codex 0.147.0.
export function matchOperation(operation) {
	const tool = text(field(operation, "tool"));
	const input = field(operation, "input");
	const proven = PROVEN_INPUTS.find((entry) => entry.tool === tool);
	if (!proven) {
		return { covered: false, tool, reason: "no CP0-proven structured input path", matched: [] };
	}
	const candidate = rebase(text(field(input, proven.path_field)), text(field(operation, "cwd")));
	if (candidate === null) {
		return { covered: false, tool, reason: "the proven field carried no path contained in the observed base", matched: [] };
	}
	return { covered: true, tool, candidate, reason: "", matched: matchRelative(candidate), evidence: proven.evidence };
}

export default { rebase, matchPath, matchOperation };
`

// renderMatcher renders the matcher module with the compiled index and the
// proven-input table inlined. It is deterministic: identical index and inputs
// produce identical bytes.
func renderMatcher(p *Plugin) ([]byte, error) {
	index, err := renderIndex(p)
	if err != nil {
		return nil, err
	}
	inputs := p.Tools
	if inputs == nil {
		inputs = []ProvenToolInput{}
	}
	data, err := json.MarshalIndent(inputs, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("codex: encode proven tool inputs: %w", err)
	}
	module := fmt.Sprintf(matcherTemplate, strings.TrimRight(string(index), "\n"), string(data))
	return []byte(module), nil
}
