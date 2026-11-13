package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderText returns the `level='resolved'` block.
func RenderText(r Resolved) string {
	var sb strings.Builder
	sb.WriteString("<workflow-preferences level='resolved'>\n")
	for _, v := range r.Values {
		fmt.Fprintf(&sb, "- %s: %s (%s)\n", v.Key, v.Value, v.Level)
	}
	if len(r.Warnings) == 0 {
		sb.WriteString("warning: none\n")
	}
	for _, w := range r.Warnings {
		fmt.Fprintf(&sb, "warning: %s\n", w)
	}
	fmt.Fprintf(&sb, "Read: %s\n", strings.Join(r.Read, ", "))
	sb.WriteString("</workflow-preferences>\n")
	return sb.String()
}

// RenderJSON carries the same facts as RenderText.
func RenderJSON(r Resolved) ([]byte, error) {
	out := struct {
		Values   []Value  `json:"values"`
		Warnings []string `json:"warnings"`
		Read     []string `json:"read"`
	}{r.Values, r.Warnings, r.Read}
	if out.Values == nil {
		out.Values = []Value{}
	}
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	if out.Read == nil {
		out.Read = []string{}
	}
	return json.Marshal(out)
}
