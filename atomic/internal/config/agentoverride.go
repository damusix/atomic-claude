package config

import "unicode"

// AgentOverride is a per-agent installer override applied to the agent's
// frontmatter at install time. Decoded only from a nested
// `[claude.agents.<name>]` table — plain struct decode, no scalar form.
type AgentOverride struct {
	Model  string `toml:"model,omitempty"`
	Effort string `toml:"effort,omitempty"`
}

// validEfforts is the Claude Code subagent effort enum: exactly these five string
// levels, no integers. The runtime downgrades gracefully per model when a level
// is not supported.
var validEfforts = map[string]bool{
	"low":    true,
	"medium": true,
	"high":   true,
	"xhigh":  true,
	"max":    true,
}

// effortOptionValues is the ordered allowlist, empty "no override" first. Reused
// by the interactive effort Select.
var effortOptionValues = []string{"", "low", "medium", "high", "xhigh", "max"}

// validModelFormat requires no internal whitespace and no ASCII control
// characters, so identifiers like "claude-opus-4-6[1m]" pass. An empty string is
// the caller's concern and treated as well-formed, so an untouched interactive
// Input still validates cleanly.
func validModelFormat(s string) bool {
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return false
		}
	}
	return true
}
