// The SSE search stream's options type and wire framing; the handler itself is
// NewAPISearchStreamHandler.
//
// It streams because a markdown grep is fast and local while federated code
// search opens one SQLite index per member, and one large member can take far
// longer than the rest. Members are searched concurrently and pushed as they
// finish: an "md" event first, then one "code" event per member in completion
// order, then exactly one "end". The client closes the EventSource on "end",
// which also stops the browser reconnecting and replaying the stream.
package serve

import (
	"fmt"
	"net/http"
	"strings"
)

// SearchStreamOptions configures NewAPISearchStreamHandler.
type SearchStreamOptions struct {
	// NavRoot is the markdown grep root.
	NavRoot string
	// RealmRoot is the code-search realm root.
	RealmRoot string
	// ClaudeMDPath lets realm.Resolve find members.
	ClaudeMDPath string
	// SearchFn nil takes DefaultMemberSearchFn.
	SearchFn MemberSearchFn
}

// normalizeSearchSrc clamps src to a known value, defaulting to "all".
func normalizeSearchSrc(src string) string {
	switch src {
	case "md", "code", "all":
		return src
	default:
		return "all"
	}
}

// writeSSE splits multi-line data across several "data:" lines, per the SSE
// spec; the browser rejoins them with newlines.
func writeSSE(w http.ResponseWriter, flusher http.Flusher, event, data string) {
	fmt.Fprintf(w, "event: %s\n", event)
	if data == "" {
		fmt.Fprint(w, "data: \n")
	} else {
		for _, line := range strings.Split(data, "\n") {
			fmt.Fprintf(w, "data: %s\n", line)
		}
	}
	fmt.Fprint(w, "\n")
	flusher.Flush()
}
