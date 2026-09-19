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
// normalization, canonical-identity matching, and uncovered-operation reporting.
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

// normalize accepts only an exact workspace-relative path. An absolute path, a
// parent escape, or an empty field yields null, and the operation is reported
// uncovered rather than matched against a path it does not have.
export function normalize(candidate) {
	if (typeof candidate !== "string" || candidate === "") return null;
	const value = candidate.replace(/\\/g, "/");
	if (value.startsWith("/") || value.startsWith("~") || /^[A-Za-z]:/.test(value)) return null;
	const segments = [];
	for (const segment of value.split("/")) {
		if (segment === "" || segment === ".") continue;
		if (segment === "..") return null;
		segments.push(segment);
	}
	if (segments.length === 0) return null;
	return segments.join("/");
}

// matchPath returns every rule identity whose compiled pattern matches an exact
// workspace-relative path, in the index's canonical identity order.
export function matchPath(candidate) {
	const path = normalize(candidate);
	if (path === null) return [];
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

// matchOperation reports whether one operation carries a proven exact target
// path. A tool absent from the proven-input table is uncovered, so it receives
// no matched rule body; an empty or unusable field is uncovered for the same
// reason. Nothing here blocks or rewrites an operation: no deny result is
// proven for Codex 0.147.0.
export function matchOperation(operation) {
	const tool = text(field(operation, "tool"));
	const input = field(operation, "input");
	const proven = PROVEN_INPUTS.find((entry) => entry.tool === tool);
	if (!proven) {
		return { covered: false, tool, reason: "no CP0-proven structured input path", matched: [] };
	}
	const candidate = normalize(text(field(input, proven.path_field)));
	if (candidate === null) {
		return { covered: false, tool, reason: "the proven field carried no usable exact path", matched: [] };
	}
	return { covered: true, tool, candidate, reason: "", matched: matchPath(candidate), evidence: proven.evidence };
}

export default { normalize, matchPath, matchOperation };
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
