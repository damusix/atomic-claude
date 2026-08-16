package config

import "unicode"

// AgentOverride is a per-agent installer override applied to the agent's
// frontmatter at install time: an optional model pin and an optional
// Claude Code subagent effort level. Decoded only from a nested
// `[claude.agents.<name>]` table — plain struct decode, no scalar form.
type AgentOverride struct {
	Model  string `toml:"model,omitempty"`
	Effort string `toml:"effort,omitempty"`
}

// validEfforts is the Claude Code subagent effort enum. Exactly these five
// string levels — no integers, no other values. Runtime downgrades
// gracefully per model when a level isn't supported.
var validEfforts = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
	"xhigh":  true,
	"max":    true,
}

// effortOptionValues is the ordered allowlist, including the empty
// "no override" choice first. Reused by the interactive effort Select.
var effortOptionValues = []string{"", "low", "medium", "high", "xhigh", "max"}

// validModelFormat reports whether a non-empty model string is well-formed:
// no internal whitespace and no ASCII control characters. Brackets, slashes,
// dots, dashes, digits, letters, and underscores are all allowed, so
// identifiers like "claude-opus-4-8" and "claude-opus-4-6[1m]" pass. An
// empty string means "no override" and is the caller's concern — it is
// treated as well-formed here so an untouched interactive Input still
// validates cleanly.
func validModelFormat(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
