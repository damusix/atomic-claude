// search_stream.go — Server-Sent Events search stream (/api/search/stream).
//
// Route: GET /api/search/stream?q=<query>&src=<md|code|all>
//
// Why streaming: a markdown grep is fast and local, but federated code search
// fans out across realm members, each opening its own SQLite index — one large
// member can take much longer than the next. Rather than block on the slowest,
// this endpoint searches members concurrently (see fanOutMembers) and pushes
// each result the moment it is ready, as a discrete SSE event. The client shows
// a loading indicator until the terminal "end" event.
//
// Events:
//   - event: md    — the markdown results block (one event; emitted first, fast).
//   - event: code  — one event per realm member, in completion order.
//   - event: end   — exactly one, last. The client clears loading and closes
//     the EventSource (which also stops the browser from
//     auto-reconnecting and replaying the stream).
//
// The handler itself lives in api_handlers.go (NewAPISearchStreamHandler);
// this file carries the shared options type and the SSE wire-framing helper.
package serve

import (
	"fmt"
	"net/http"
	"strings"
)

// SearchStreamOptions configures NewAPISearchStreamHandler.
type SearchStreamOptions struct {
	// NavRoot is the markdown grep root (same as the md search handler).
	NavRoot string
	// RealmRoot is the code-search realm root (same as the code search handler).
	RealmRoot string
	// ClaudeMDPath is used by realm.Resolve to find members.
	ClaudeMDPath string
	// SearchFn is the per-member code search seam. nil → DefaultMemberSearchFn().
	SearchFn MemberSearchFn
}

// normalizeSearchSrc clamps the src param to a known value (default "all").
func normalizeSearchSrc(src string) string {
	switch src {
	case "md", "code", "all":
		return src
	default:
		return "all"
	}
}

// writeSSE writes one Server-Sent Event. Multi-line data is split into multiple
// "data:" lines per the SSE spec; the browser rejoins them with "\n".
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
