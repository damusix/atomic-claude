// Phoenix (Elixir) router resolver.
//
// Known gaps: `scope "/prefix" do` blocks are not expanded, so a scoped route
// records the path as written rather than its real prefixed form, and the
// resources macro is not expanded at all.
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

// phoenixVerbRe matches `get "/p", SomeController, :action` in both the space
// and paren call forms. Groups: verb, path, action atom. Horizontal-whitespace
// classes are used throughout rather than \s, so a match cannot start on one
// line and be attributed to another.
var phoenixVerbRe = regexp.MustCompile(
	`(?m)^[^\S\n]*(get|post|put|patch|delete)(?:[^\S\n]+|\()[^\S\n]*` +
		`"([^"]+)"\s*,\s*` + // double-quoted path
		`[A-Za-z][A-Za-z0-9_.]*\s*,\s*` + // Controller module (ignored)
		`:([A-Za-z_][A-Za-z0-9_]*)`, // :action atom (captured without ':')
)

type PhoenixResolver struct {
	projectRoot string
	claimed     map[string]bool
}

func NewPhoenixResolver(projectRoot string) *PhoenixResolver {
	return &PhoenixResolver{projectRoot: projectRoot, claimed: make(map[string]bool)}
}

func (r *PhoenixResolver) Name() string { return "phoenix" }

func (r *PhoenixResolver) Languages() []types.Language {
	return []types.Language{types.LanguageElixir}
}

// Detect matches the Mix dependency tuple form so a mere mention of phoenix
// elsewhere in mix.exs does not count.
func (r *PhoenixResolver) Detect(ctx context.Context) bool {
	data, err := os.ReadFile(filepath.Join(r.projectRoot, "mix.exs"))
	if err != nil {
		return false
	}
	return strings.Contains(string(data), "{:phoenix,")
}

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

		node := MakeRouteNode(filePath, line, verb, routePath, types.LanguageElixir)
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
				Language:      types.LanguageElixir,
			})
		}
	}

	return nodes, refs
}

func (r *PhoenixResolver) ClaimsReference(name string) bool { return r.claimed[name] }

func (r *PhoenixResolver) Resolve(ctx context.Context, ref types.UnresolvedReference) (resolution.ResolvedRef, error) {
	if !r.claimed[ref.ReferenceName] {
		return resolution.ResolvedRef{}, nil
	}
	return resolution.ResolvedRef{Confidence: 0.85}, nil
}
