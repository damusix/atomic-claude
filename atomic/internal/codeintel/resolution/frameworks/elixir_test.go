package frameworks_test

// Failing-first TDD tests for CP15 batch E (Elixir): phoenix resolver.
//
// NOTE ON LANGUAGE: Elixir is NOT in the 29-language set (appendix C).
// Route nodes and refs use types.LanguageUnknown. Languages() returns
// [types.LanguageUnknown]. This is documented in elixir.go.
//
// Coverage:
//  1. Detect true on mix.exs with :phoenix dep + false otherwise.
//  2. Extract: get/post/put/patch/delete → uppercase method + :action atom handler ref.
//  3. Route nodes carry LanguageUnknown (Elixir absent from appendix C).
//  4. A `# get "/x"` commented-out route emits NOTHING (# line stripping).
//  5. Resolve 0.8–0.9 + ClaimsReference.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/resolution/frameworks"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

func TestPhoenixDetect_MixExs(t *testing.T) {
	dir := t.TempDir()
	mixExs := `defmodule MyApp.MixProject do
  use Mix.Project

  def application do
    [extra_applications: [:logger], mod: {MyApp.Application, []}]
  end

  defp deps do
    [
      {:phoenix, "~> 1.7"},
      {:phoenix_ecto, "~> 4.4"},
      {:ecto_sql, "~> 3.10"}
    ]
  end
end
`
	if err := os.WriteFile(filepath.Join(dir, "mix.exs"), []byte(mixExs), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewPhoenixResolver(dir)
	if !r.Detect(context.Background()) {
		t.Error("Phoenix.Detect should return true when mix.exs lists :phoenix")
	}
}

func TestPhoenixDetect_False(t *testing.T) {
	dir := t.TempDir()
	// Elixir project without Phoenix
	mixExs := `defmodule OtherApp.MixProject do
  defp deps do
    [{:ecto, "~> 3.10"}]
  end
end
`
	if err := os.WriteFile(filepath.Join(dir, "mix.exs"), []byte(mixExs), 0o644); err != nil {
		t.Fatal(err)
	}
	r := frameworks.NewPhoenixResolver(dir)
	if r.Detect(context.Background()) {
		t.Error("Phoenix.Detect should return false when mix.exs does not list :phoenix")
	}
}

func TestPhoenixExtract_GetRoute(t *testing.T) {
	r := frameworks.NewPhoenixResolver(t.TempDir())
	src := `defmodule MyAppWeb.Router do
  use MyAppWeb, :router

  pipeline :browser do
    plug :accepts, ["html"]
  end

  scope "/", MyAppWeb do
    pipe_through :browser
    get "/", PageController, :index
  end
end
`
	nodes, refs := r.Extract("lib/my_app_web/router.ex", src)

	if len(nodes) == 0 {
		t.Fatal("expected at least 1 route node, got 0")
	}
	// Find the GET / node
	var found *types.Node
	for i := range nodes {
		if strings.Contains(nodes[i].ID, "GET:/") {
			found = &nodes[i]
		}
	}
	if found == nil {
		t.Fatalf("no GET route found; nodes: %v", elixirNodeIDs(nodes))
	}

	// Language MUST be LanguageUnknown (Elixir absent from appendix C)
	if found.Language != types.LanguageUnknown {
		t.Errorf("route node Language = %v, want LanguageUnknown (Elixir absent from appendix C)", found.Language)
	}
	if found.Kind != types.NodeKindRoute {
		t.Errorf("route node Kind = %v, want Route", found.Kind)
	}

	// Handler ref: :index atom → ref name "index"
	foundRef := false
	for _, ref := range refs {
		if ref.ReferenceName == "index" {
			foundRef = true
			if ref.FromNodeID != found.ID {
				t.Errorf("ref FromNodeID = %q, want %q", ref.FromNodeID, found.ID)
			}
		}
	}
	if !foundRef {
		t.Errorf("expected ref to 'index' (from :index atom), refs: %v", refs)
	}
}

func TestPhoenixExtract_LanguageIsUnknown(t *testing.T) {
	// This test explicitly proves the LanguageUnknown contract for ALL emitted nodes.
	r := frameworks.NewPhoenixResolver(t.TempDir())
	src := `  get "/users", UserController, :index
  post "/users", UserController, :create
`
	nodes, _ := r.Extract("lib/web/router.ex", src)

	if len(nodes) == 0 {
		t.Fatal("expected nodes, got 0")
	}
	for _, n := range nodes {
		if n.Language != types.LanguageUnknown {
			t.Errorf("node %q language = %v, want LanguageUnknown", n.ID, n.Language)
		}
	}
}

func TestPhoenixExtract_CommentedRouteEmitsNothing(t *testing.T) {
	// A route prefixed with # is a comment in Elixir — must NOT emit a node.
	r := frameworks.NewPhoenixResolver(t.TempDir())
	src := `  # get "/secret", SecretController, :index
  get "/public", PublicController, :show
`
	nodes, _ := r.Extract("lib/web/router.ex", src)
	if len(nodes) != 1 {
		t.Errorf("commented route must emit nothing; got %d nodes: %v", len(nodes), elixirNodeIDs(nodes))
	}
	if !strings.Contains(nodes[0].ID, "GET:/public") {
		t.Errorf("unexpected node ID: %s", nodes[0].ID)
	}
}

func TestPhoenixExtract_AllVerbsUppercase(t *testing.T) {
	r := frameworks.NewPhoenixResolver(t.TempDir())
	src := `  get     "/a", AController, :a
  post    "/b", BController, :b
  put     "/c", CController, :c
  patch   "/d", DController, :d
  delete  "/e", EController, :e
`
	nodes, _ := r.Extract("lib/web/router.ex", src)
	if len(nodes) != 5 {
		t.Fatalf("expected 5 nodes, got %d", len(nodes))
	}
	want := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
	for i, n := range nodes {
		parts := strings.SplitN(n.Name, " ", 2)
		if parts[0] != want[i] {
			t.Errorf("node[%d] method = %q, want %q", i, parts[0], want[i])
		}
	}
}

func TestPhoenixExtract_NoMatchUnrelated(t *testing.T) {
	r := frameworks.NewPhoenixResolver(t.TempDir())
	nodes, refs := r.Extract("lib/my_app/accounts.ex", `defmodule MyApp.Accounts do
  def get_user(id), do: Repo.get(User, id)
end
`)
	if len(nodes) != 0 || len(refs) != 0 {
		t.Errorf("non-router file should emit nothing, got %d nodes %d refs", len(nodes), len(refs))
	}
}

func TestPhoenixResolve(t *testing.T) {
	r := frameworks.NewPhoenixResolver(t.TempDir())
	src := `  get "/ping", PingController, :ping
`
	r.Extract("lib/web/router.ex", src)

	ref := types.UnresolvedReference{ReferenceName: "ping"}
	resolved, err := r.Resolve(context.Background(), ref)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if resolved.Confidence < 0.8 || resolved.Confidence > 0.9 {
		t.Errorf("Resolve confidence = %v, want 0.8–0.9", resolved.Confidence)
	}
	if !r.ClaimsReference("ping") {
		t.Error("ClaimsReference should return true after Extract sees 'ping'")
	}
}

// elixirNodeIDs is a test helper.
func elixirNodeIDs(nodes []types.Node) []string {
	ids := make([]string, len(nodes))
	for i, n := range nodes {
		ids[i] = n.ID
	}
	return ids
}
