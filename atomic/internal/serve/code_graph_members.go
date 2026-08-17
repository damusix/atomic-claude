// GET /code/graph/members: the served scope's code members and their indexed
// state, for the code view's member picker.
//
// Repo scope returns exactly one member with an empty prefix. The client
// renders the picker only when more than one comes back, which is what makes
// single-repo mode picker-free.
package serve

import (
	"encoding/json"
	"net/http"
)

type graphMember struct {
	Key     string `json:"key"`
	Prefix  string `json:"prefix"`
	Indexed bool   `json:"indexed"`
}

type graphMembersResponse struct {
	Members []graphMember `json:"members"`
}

type codeGraphMembersHandler struct {
	memberResolver
}

// NewCodeGraphMembersHandler shares CodeGraphOptions rather than a bespoke
// type: the member-discovery seam is identical.
func NewCodeGraphMembersHandler(opts CodeGraphOptions) http.Handler {
	return &codeGraphMembersHandler{
		memberResolver: memberResolver{
			realmRoot:     opts.RealmRoot,
			claudeMDPath:  opts.ClaudeMDPath,
			wikiIndexPath: opts.WikiIndexPath,
		},
	}
}

// ServeHTTP settles Indexed with a file-existence check rather than an engine
// open: the picker needs to know a member is selectable, not what it holds.
func (h *codeGraphMembersHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	members := h.members()
	resp := graphMembersResponse{Members: make([]graphMember, 0, len(members))}
	for _, m := range members {
		resp.Members = append(resp.Members, graphMember{
			Key:     m.Key,
			Prefix:  m.Prefix,
			Indexed: fileExists(m.DBPath),
		})
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(resp)
}
