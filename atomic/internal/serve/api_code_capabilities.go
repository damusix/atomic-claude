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
// Only the SQL schema view so far. Most repositories have no SQL, and a
// permanent mode that can only say "nothing here" is a promise the tool cannot
// keep, so the view appears when the index actually holds SQL objects.
// `[serve] schema` in .claude/atomic.toml overrides that either way, for SQL
// the indexer cannot see and for repos whose few migration files are not what
// their authors want the tool to be about.

type apiCapabilitiesResponse struct {
	Schema bool `json:"schema"`
	// Source is "config" or "detected", so a surprising answer is traceable to
	// a file rather than to a guess.
	Source string `json:"source"`
}

// Detection opens every member's index, so it is memoized. The TTL exists only
// so a reindex started from the UI is picked up without a restart.
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

// hasSQLObjects counts procedures too: a database defined largely in stored
// routines has them even where its tables live elsewhere.
func hasSQLObjects(ctx context.Context, eng CodeEngine) bool {
	for _, k := range []types.NodeKind{types.NodeKindTable, types.NodeKindView, types.NodeKindProcedure} {
		nodes, err := eng.GetNodesByKind(ctx, k)
		if err != nil {
			continue // a failed probe is not a "no"
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

	// Repo scope has no members, so probe the served root's own index.
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
