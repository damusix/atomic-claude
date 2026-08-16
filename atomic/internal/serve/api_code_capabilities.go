package serve

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/config"
)

// GET /api/code/capabilities — what the shell should offer for this scope.
//
// Only the SQL schema view so far. Most repositories have no SQL at all, and a
// permanent mode that can only ever say "nothing here" is worse than no mode:
// it is a promise the tool cannot keep. So the view exists when the index
// actually holds SQL objects, and `[serve] schema` in .claude/atomic.toml
// overrides that in either direction for the cases detection gets wrong —
// SQL that lives somewhere the indexer does not read, or a repo whose handful
// of migration files are not what its authors want the tool to be about.

type apiCapabilitiesResponse struct {
	Schema bool `json:"schema"`
	// Source says how Schema was decided: "config" when .claude/atomic.toml
	// set it outright, "detected" otherwise. Surfaced so a surprising answer
	// is traceable to a file rather than to a guess.
	Source string `json:"source"`
}

// Detection opens every member's index, so it is memoized. The answer only
// changes when an index is rebuilt, which a running server does not do behind
// its own back — the TTL exists so a reindex started from the UI is picked up
// without a restart.
const capabilitiesTTL = 60 * time.Second

type capabilitiesCache struct {
	mu   sync.Mutex
	now  func() time.Time
	at   time.Time
	resp *apiCapabilitiesResponse
}

func (c *capabilitiesCache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *capabilitiesCache) get(compute func() apiCapabilitiesResponse) apiCapabilitiesResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.resp != nil && c.clock().Sub(c.at) < capabilitiesTTL {
		return *c.resp
	}
	resp := compute()
	c.at, c.resp = c.clock(), &resp
	return resp
}

// hasSQLObjects reports whether an index holds anything this view could show.
// Procedures count: a database defined largely in stored routines has them
// even in a member whose tables live elsewhere.
func hasSQLObjects(ctx context.Context, eng CodeEngine) bool {
	for _, k := range []types.NodeKind{types.NodeKindTable, types.NodeKindView, types.NodeKindProcedure} {
		nodes, err := eng.GetNodesByKind(ctx, k)
		if err != nil {
			continue // best-effort: a failed probe is not a "no"
		}
		for _, n := range nodes {
			if n.Language == types.LanguageSQL {
				return true
			}
		}
	}
	return false
}

func (h *apiCodeExplorerHandler) computeCapabilities(ctx context.Context) apiCapabilitiesResponse {
	if cfg, _, err := config.LoadRepoConfig(config.RepoConfigPath(h.realmRoot)); err == nil && cfg.Serve.Schema != nil {
		return apiCapabilitiesResponse{Schema: *cfg.Serve.Schema, Source: "config"}
	}

	for _, m := range h.members() {
		eng, err := h.openEngineFor(ctx, m)
		if err != nil {
			continue
		}
		found := hasSQLObjects(ctx, eng)
		eng.Close()
		if found {
			return apiCapabilitiesResponse{Schema: true, Source: "detected"}
		}
	}

	// Repo scope has no members list — probe the served root's own index.
	eng, err := h.provider(ctx, h.realmRoot, h.localDBPath())
	if err != nil {
		return apiCapabilitiesResponse{Schema: false, Source: "detected"}
	}
	defer eng.Close()
	return apiCapabilitiesResponse{Schema: hasSQLObjects(ctx, eng), Source: "detected"}
}

func (h *apiCodeExplorerHandler) handleAPICapabilities(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	writeAPIJSON(w, h.capabilities.get(func() apiCapabilitiesResponse {
		return h.computeCapabilities(ctx)
	}))
}
