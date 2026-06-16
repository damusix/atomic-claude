// search_stream.go — Server-Sent Events search stream (/search/stream).
//
// Route: GET /search/stream?q=<query>&src=<md|code|all>
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
// The dialog uses the plain fetch endpoints (/search/md, /code/search) for live,
// debounced quick-jump; this stream backs the dedicated /search page, where the
// user has committed to browsing all results and progressive arrival matters.
package serve

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/realm"
)

// SearchStreamOptions configures NewSearchStreamHandler.
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

// NewSearchStreamHandler returns an http.Handler for GET /search/stream.
func NewSearchStreamHandler(opts SearchStreamOptions) http.Handler {
	fn := opts.SearchFn
	if fn == nil {
		fn = DefaultMemberSearchFn()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		q := strings.TrimSpace(r.URL.Query().Get("q"))
		src := normalizeSearchSrc(r.URL.Query().Get("src"))

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no") // defeat any intermediary buffering
		w.WriteHeader(http.StatusOK)

		ctx := r.Context()

		if q == "" {
			writeSSE(w, flusher, "end", "")
			return
		}

		// Markdown: fast local grep → one event.
		if src == "md" || src == "all" {
			mh := &mdSearchHandler{navRoot: opts.NavRoot}
			matches, truncated := mh.search(q)
			var sb strings.Builder
			renderMdResults(&sb, q, matches, truncated)
			writeSSE(w, flusher, "md", sb.String())
		}

		// Code: per-member, streamed as each concurrent query completes.
		if src == "code" || src == "all" {
			streamCodeResults(ctx, w, flusher, opts.RealmRoot, opts.ClaudeMDPath, q, fn)
		}

		writeSSE(w, flusher, "end", "")
	})
}

// streamCodeResults resolves the realm and emits one "code" SSE event per member
// as its concurrent search completes. A realm with no code members emits a single
// not-indexed note so the client never sees a silent empty section.
func streamCodeResults(
	ctx context.Context,
	w http.ResponseWriter,
	flusher http.Flusher,
	realmRoot, claudeMDPath, query string,
	fn MemberSearchFn,
) {
	res, err := realm.Resolve(realmRoot, claudeMDPath)
	if err != nil {
		writeSSE(w, flusher, "code", codeSearchNoIndexNote())
		return
	}

	emitted := false
	groups := codeSearchGroups(ctx, res, realmRoot, nil, nil, query, fn, func(g memberResult) {
		var sb strings.Builder
		renderMemberGroup(&sb, g)
		writeSSE(w, flusher, "code", sb.String())
		emitted = true
	})

	if len(groups) == 0 && !emitted {
		writeSSE(w, flusher, "code", codeSearchNoIndexNote())
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
