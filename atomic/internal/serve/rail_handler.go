// rail_handler.go — right-rail data shared by the /api/rail JSON handler
// (api_handlers.go): YAML frontmatter Properties (source order), outbound
// links, and backlinks for the focused page.
package serve

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

// propKV is a key/value pair for the Properties slot.
//
// Scalars (frontmatter.ParseOrdered returns them as strings) pass through as text.
// Non-primitive values — arrays and nested objects — are pretty-printed as JSON
// (IsJSON), instead of the unreadable fmt default ("map[Key:val ...]").
//
// When IsURL is true, Value is an http(s) URL and the client renders it as a
// link.
type propKV struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	IsURL  bool   `json:"isURL"`
	IsJSON bool   `json:"isJSON"`
}

// isHTTPURL reports whether s is an http:// or https:// URL. Used to detect
// frontmatter values that should render as clickable anchors in the Properties
// slot. We deliberately choose model-free detection (prefix check) rather than
// url.Parse: frontmatter values that look like URLs but are not valid RFC-3986
// URIs are still useful to render as links (the browser validates on click).
func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// railProperties reads abs and parses its YAML frontmatter into the ordered
// Properties slot data for the /api/rail JSON handler (api_handlers.go). A
// read or parse error degrades to nil (no properties) rather than an error:
// the caller has already confirmed the page exists via graph membership.
func railProperties(abs string) []propKV {
	fileData, readErr := readFile(abs)
	if readErr != nil {
		return nil
	}
	kvs, _, fmErr := frontmatter.ParseOrdered(string(fileData))
	if fmErr != nil {
		return nil
	}

	var props []propKV
	for _, kv := range kvs {
		// Primitive scalar (ParseOrdered yields these as strings): plain text.
		if s, ok := kv.Value.(string); ok {
			props = append(props, propKV{Key: kv.Key, Value: s, IsURL: isHTTPURL(s)})
			continue
		}
		// Non-primitive (array / object): pretty-print as JSON in a
		// highlighted block. ParseOrdered values are JSON-safe
		// (string / []any / map[string]any), so marshal cannot hit an
		// unsupported type; degrade to fmt only on the unexpected.
		if b, jerr := json.MarshalIndent(kv.Value, "", "  "); jerr == nil {
			props = append(props, propKV{Key: kv.Key, Value: string(b), IsJSON: true})
		} else {
			props = append(props, propKV{Key: kv.Key, Value: fmt.Sprint(kv.Value)})
		}
	}
	return props
}
