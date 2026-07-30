// code_graph_members.go — code-graph spec CP6: member discovery for the code
// view's realm member picker (SC7).
//
// Route: GET /code/graph/members
//
// Returns the served scope's code members with each one's indexed state,
// reusing the same memberResolver seam /code/graph/data and the sibling
// /code/* explorer routes use. Repo scope always comes back as exactly one
// member with an empty prefix — the FE's contract (see code-graph.js) is
// "render the picker only when more than one member is returned", so a
// single-member response is what makes single-repo mode picker-free.
package serve

import (
	"encoding/json"
	"net/http"
)

// graphMember is one member entry in the /code/graph/members response.
type graphMember struct {
	Key     string `json:"key"`
	Prefix  string `json:"prefix"`
	Indexed bool   `json:"indexed"`
}

// graphMembersResponse is the /code/graph/members success payload.
type graphMembersResponse struct {
	Members []graphMember `json:"members"`
}

// codeGraphMembersHandler implements http.Handler for GET /code/graph/members.
type codeGraphMembersHandler struct {
	memberResolver
}

// NewCodeGraphMembersHandler returns an http.Handler for GET /code/graph/members.
// Reuses CodeGraphOptions (same RealmRoot/ClaudeMDPath/WikiIndexPath shape as
// NewCodeGraphHandler) rather than a bespoke options type — the member
// discovery seam is identical.
func NewCodeGraphMembersHandler(opts CodeGraphOptions) http.Handler {
	return &codeGraphMembersHandler{
		memberResolver: memberResolver{
			realmRoot:     opts.RealmRoot,
			claudeMDPath:  opts.ClaudeMDPath,
			wikiIndexPath: opts.WikiIndexPath,
		},
	}
}

// ServeHTTP lists every discovered member with its indexed state. Indexed is
// a cheap file-existence check (fileExists, code_members.go) — no engine open
// — since the picker only needs to know whether a member is selectable, not
// its actual graph content.
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
