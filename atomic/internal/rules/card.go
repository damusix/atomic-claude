package rules

import (
	"encoding/json"
	"strings"
)

// CardDescription is the one-line summary OMP lists for a wiki card: the first
// non-blank body line without heading marks, else a name-derived fallback.
func CardDescription(domain, body string) string {
	for _, line := range strings.Split(body, "\n") {
		if line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "#")); line != "" {
			return line
		}
	}
	return "Atomic wiki card for " + domain
}

// YAMLString quotes s as a YAML double-quoted scalar; a JSON string is one.
func YAMLString(s string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(b.String(), "\n")
}
