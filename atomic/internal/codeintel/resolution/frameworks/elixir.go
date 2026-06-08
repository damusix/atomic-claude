// Package frameworks — Elixir framework resolver (CP15 batch E).
//
// This file implements one FrameworkResolver + FrameworkExtractor pair for
// the Phoenix web framework (Elixir).
//
// # Language — IMPORTANT
//
// Elixir is NOT in the 29-language set (appendix C of the code-intel-engine
// spec). Route nodes and handler refs use types.LanguageUnknown. Languages()
// returns [types.LanguageUnknown]. ClaimsReference and Resolve are implemented
// for contract completeness even though no Elixir-language refs come from
// extraction.
//
// # Comment stripping
//
// Elixir uses # for single-line comments. stripHashLineComments (defined in
// ruby.go, which calls stripPyComments from python.go) strips # lines.
// Its triple-quote handling is harmless on Elixir router files.
//
// # Route node format (appendix H — via MakeRouteNode)
//
//	id:            route:{filePath}:{line}:{METHOD}:{path}
//	qualifiedName: {filePath}::METHOD:{path}
//	name:          "METHOD /path"
//
// # Phoenix router DSL idioms
//
// Supported forms in lib/*_web/router.ex:
//
//	get  "/path", PageController, :action
//	post "/path", PageController, :action
//	put  "/path", PageController, :action
//	patch "/path", PageController, :action
//	delete "/path", PageController, :action
//
// HTTP verbs: get post put patch delete → uppercase in route node name.
// Handler = the :action atom (strip the leading ':').
//
// Optional `scope "/prefix" do ... end` prefix expansion is NOT supported
// (best-effort is documented as acceptable). Paths are recorded as-is from
// the route line. A future enhancement could walk scope blocks.
//
// resources/resource macro expansion is NOT supported (same rationale as Rails).
//
// # Detect
//
// Primary: mix.exs in projectRoot contains `:phoenix` as a dependency key
// (matches `{:phoenix,` which is the standard Mix dependency tuple syntax).
package frameworks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ---------------------------------------------------------------------------
// Phoenix regexes
// ---------------------------------------------------------------------------

// phoenixVerbRe matches Phoenix router DSL verb lines:
//
//	get  "/path", SomeController, :action
//	post "/path", SomeController, :action
//
// Capture groups:
//
//	1 — HTTP verb (get|post|put|patch|delete)
//	2 — route path (double-quoted)
//	3 — :action atom (with leading ':')
//
// phoenixVerbRe uses [^\S\n]* (spaces/tabs only, not newline) so the match
// starts on the correct line for accurate line-number calculation.
var phoenixVerbRe = regexp.MustCompile(
	`(?m)^[^\S\n]*(get|post|put|patch|delete)[^\S\n]+` +
		`"([^"]+)"\s*,\s*` + // double-quoted path
		`[A-Za-z][A-Za-z0-9_.]*\s*,\s*` + // Controller module (ignored)
		`:([A-Za-z_][A-Za-z0-9_]*)`, // :action atom (captured without ':')
)

// ---------------------------------------------------------------------------
// Phoenix resolver
// ---------------------------------------------------------------------------

// PhoenixResolver implements FrameworkResolver + FrameworkExtractor for Phoenix.
//
// NOTE: Elixir is absent from the 29-language set (appendix C). All route
// nodes and refs use types.LanguageUnknown. See package doc.
type PhoenixResolver struct {
	projectRoot string
	claimed     map[string]bool
}

// NewPhoenixResolver creates a PhoenixResolver.
func NewPhoenixResolver(projectRoot string) *PhoenixResolver {
	return &PhoenixResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

// Name returns "phoenix".
func (r *PhoenixResolver) Name() string { return "phoenix" }

// Languages returns [LanguageUnknown] because Elixir is not in the 29-language
// set (appendix C). Route nodes and refs use LanguageUnknown for the same reason.
func (r *PhoenixResolver) Languages() []types.Language {
	return []types.Language{types.LanguageUnknown}
}

// Detect returns true when mix.exs in projectRoot contains `:phoenix` as a
// dependency, identified by the string `{:phoenix,` (standard Mix dep tuple).
func (r *PhoenixResolver) Detect(ctx context.Context) bool {
	data, err := os.ReadFile(filepath.Join(r.projectRoot, "mix.exs"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "{:phoenix,")
}

// Extract scans filePath/content for Phoenix router verb lines and returns
// route nodes + handler refs. # line comments are stripped first.
//
// Route nodes carry LanguageUnknown (Elixir absent from appendix C).
func (r *PhoenixResolver) Extract(filePath, content string) ([]types.Node, []types.UnresolvedReference) {
	stripped := stripHashLineComments(content)
	totalLines := strings.Count(content, "\n") + 1

	var nodes []types.Node
	var refs []types.UnresolvedReference

	for _, loc := range phoenixVerbRe.FindAllStringSubmatchIndex(stripped, -1) {
		if len(loc) < 8 {
			continue
		}
		verb := strings.ToUpper(stripped[loc[2]:loc[3]])
		routePath := stripped[loc[4]:loc[5]]
		action := stripped[loc[6]:loc[7]]

		line := strings.Count(stripped[:loc[0]], "\n") + 1
		if line > totalLines {
			line = totalLines
		}

		// LanguageUnknown: Elixir is not in the 29-language set (appendix C).
		node := MakeRouteNode(filePath, line, verb, routePath, types.LanguageUnknown)
		nodes = append(nodes, node)

		if action != "" {
			r.claimed[action] = true
			refs = append(refs, types.UnresolvedReference{
				ID:            fmt.Sprintf("ref:%s:%d:%s:%s", filePath, line, verb, action),
				FromNodeID:    node.ID,
				ReferenceName: action,
				ReferenceKind: types.EdgeKindReferences,
				Line:          line,
				FilePath:      filePath,
				Language:      types.LanguageUnknown,
			})
		}
	}

	return nodes, refs
}

// ClaimsReference returns true if an action atom with this name was seen.
func (r *PhoenixResolver) ClaimsReference(name string) bool { return r.claimed[name] }

// Resolve returns confidence 0.85 for claimed action names.
// Implemented for contract completeness (no Elixir-language refs come from
// extraction since Elixir is absent from appendix C).
func (r *PhoenixResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}
