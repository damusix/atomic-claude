// Right-rail data behind /api/rail: the focused page's frontmatter properties
// in source order, its outbound links, and its backlinks.
package serve

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/frontmatter"
)

// propKV is one Properties-slot entry. Scalars pass through as text; arrays
// and objects are pretty-printed JSON rather than fmt's unreadable
// "map[Key:val ...]". IsURL tells the client to render Value as a link.
type propKV struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	IsURL  bool   `json:"isURL"`
	IsJSON bool   `json:"isJSON"`
}

// isHTTPURL is a prefix check rather than url.Parse: a frontmatter value that
// looks like a URL but is not valid RFC 3986 is still worth linking, and the
// browser validates it on click.
func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// railProperties degrades to nil rather than erroring: the caller already
// confirmed the page exists via graph membership.
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
		if s, ok := kv.Value.(string); ok {
			props = append(props, propKV{Key: kv.Key, Value: s, IsURL: isHTTPURL(s)})
			continue
		}
		// ParseOrdered only yields JSON-safe values, so the fmt fallback below
		// is for the genuinely unexpected.
		if b, jerr := json.MarshalIndent(kv.Value, "", "  "); jerr == nil {
			props = append(props, propKV{Key: kv.Key, Value: string(b), IsJSON: true})
		} else {
			props = append(props, propKV{Key: kv.Key, Value: fmt.Sprint(kv.Value)})
		}
	}
	return props
}
