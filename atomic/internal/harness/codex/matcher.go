package codex

import (
	"encoding/json"
	"fmt"
	"strings"
)

// matcherTemplate is the matcher module the plugin publishes. It registers no
// hook, because Codex 0.147.0 proved no event to wire it to.
const matcherTemplate = `// Atomic's Codex rule matcher. Generated from the projected rule corpus by the
// atomic binary; do not edit.

// Codex 0.147.0 proved no hook surface, so nothing wires this module; a shell
// command string is never parsed.

const INDEX = %s;
const PROVEN_INPUTS = %s;

function text(value) {
	return typeof value === "string" ? value : "";
}

function field(object, key) {
	if (typeof object !== "object" || object === null) return undefined;
	return object[key];
}

// A ".." above the root yields null: the path leaves the base and cannot be
// expressed as a base-relative candidate.
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

// rebase returns the base-relative candidate the canonical matcher takes, or
// null when the path cannot be placed inside the observed base.
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

// matchPath returns the rule identities matching a path rebased onto the
// observed base; a path the base does not contain matches nothing.
export function matchPath(candidate, base) {
	const path = rebase(candidate, base);
	if (path === null) return [];
	return matchRelative(path);
}

// matchOperation reports whether one operation carries a proven exact target
// path. It never blocks: no deny result is proven for Codex 0.147.0.
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
// proven-input table inlined, deterministically.
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
