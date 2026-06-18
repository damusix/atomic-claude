package frameworks_test

// Tests for CP15 batch E (Elixir): phoenix resolver.
//
// NOTE ON LANGUAGE: Elixir is a supported language (types.LanguageElixir).
// Route nodes and refs carry LanguageElixir. Languages() returns
// [types.LanguageElixir]. getApplicableResolvers(LanguageElixir) includes
// this resolver so Extract runs on indexed .ex router files.
//
// Coverage:
//  1. Detect true on mix.exs with :phoenix dep + false otherwise.
//  2. Extract: get/post/put/patch/delete → uppercase method + :action atom handler ref.
//  3. Route nodes carry LanguageElixir.
//  4. A `# get "/x"` commented-out route emits NOTHING (# line stripping).
//  5. Resolve 0.8–0.9 + ClaimsReference.
//  6. Representative realworld router.ex fixture: routes + refs carry LanguageElixir.
//  7. getApplicableResolvers(LanguageElixir) includes PhoenixResolver.

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

	// Language MUST be LanguageElixir (Elixir is a supported language).
	if found.Language != types.LanguageElixir {
		t.Errorf("route node Language = %v, want LanguageElixir", found.Language)
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

func TestPhoenixExtract_LanguageIsElixir(t *testing.T) {
	// Proves LanguageElixir on ALL emitted nodes and refs now that Elixir is supported.
	r := frameworks.NewPhoenixResolver(t.TempDir())
	src := `  get "/users", UserController, :index
  post "/users", UserController, :create
`
	nodes, refs := r.Extract("lib/web/router.ex", src)

	if len(nodes) == 0 {
		t.Fatal("expected nodes, got 0")
	}
	for _, n := range nodes {
		if n.Language != types.LanguageElixir {
			t.Errorf("node %q language = %v, want LanguageElixir", n.ID, n.Language)
		}
	}
	for _, ref := range refs {
		if ref.Language != types.LanguageElixir {
			t.Errorf("ref %q language = %v, want LanguageElixir", ref.ID, ref.Language)
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

// TestPhoenixExtract_ReporterRouterFixture exercises a representative
// gothinkster-style Phoenix realworld router: scope + pipe_through + several
// verb lines. Asserts specific routes and refs are produced with LanguageElixir.
// The test fails if extraction regresses to 0 routes.
func TestPhoenixExtract_ReporterRouterFixture(t *testing.T) {
	r := frameworks.NewPhoenixResolver(t.TempDir())

	// Modelled on the gothinkster elixir-phoenix realworld router.ex.
	// scope "/api" and pipe_through are NOT expanded (documented best-effort),
	// so paths are recorded as-is from the verb line.
	src := `defmodule ConduitWeb.Router do
  use ConduitWeb, :router

  pipeline :api do
    plug :accepts, ["json"]
    plug ConduitWeb.Auth
  end

  scope "/api", ConduitWeb do
    pipe_through :api

    get  "/articles",           ArticleController, :index
    post "/articles",           ArticleController, :create
    get  "/articles/:slug",     ArticleController, :show
    put  "/articles/:slug",     ArticleController, :update
    delete "/articles/:slug",   ArticleController, :delete
    post "/articles/:slug/comments", CommentController, :create
    get  "/profiles/:username", ProfileController, :show
    # get "/hidden", HiddenController, :index
  end
end
`
	filePath := "lib/conduit_web/router.ex"
	nodes, refs := r.Extract(filePath, src)

	// Must produce non-zero routes — regression guard.
	if len(nodes) == 0 {
		t.Fatal("router.ex fixture produced 0 route nodes; extraction regressed")
	}

	// Build lookup maps for O(1) checks.
	nodesByNamePart := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		nodesByNamePart[n.Name] = true
	}
	refsByName := make(map[string]bool, len(refs))
	for _, ref := range refs {
		refsByName[ref.ReferenceName] = true
	}

	// Assert specific routes.
	wantRoutes := []string{
		"GET /articles",
		"POST /articles",
		"GET /articles/:slug",
		"PUT /articles/:slug",
		"DELETE /articles/:slug",
		"POST /articles/:slug/comments",
		"GET /profiles/:username",
	}
	for _, want := range wantRoutes {
		if !nodesByNamePart[want] {
			t.Errorf("expected route node %q; nodes produced: %v", want, elixirNodeNames(nodes))
		}
	}

	// Commented-out route must NOT appear.
	if nodesByNamePart["GET /hidden"] {
		t.Error("commented-out route GET /hidden must not be extracted")
	}

	// Assert specific handler refs.
	wantRefs := []string{"index", "create", "show", "update", "delete"}
	for _, want := range wantRefs {
		if !refsByName[want] {
			t.Errorf("expected ref %q; refs produced: %v", want, elixirRefNames(refs))
		}
	}

	// All nodes and refs must carry LanguageElixir.
	for _, n := range nodes {
		if n.Language != types.LanguageElixir {
			t.Errorf("node %q language = %v, want LanguageElixir", n.Name, n.Language)
		}
	}
	for _, ref := range refs {
		if ref.Language != types.LanguageElixir {
			t.Errorf("ref %q language = %v, want LanguageElixir", ref.ReferenceName, ref.Language)
		}
	}
}

// TestPhoenixExtract_ParenFormRoutes proves that paren-form route macros
// (e.g. get("/openapi", …), post("/x", …)) are extracted identically to the
// space form. This is the regression test for the bug found in Plausible's
// router where `get("/openapi", …)` was silently dropped because the old regex
// required at least one horizontal whitespace char between the verb and the
// opening quote, making `get("` a non-match.
func TestPhoenixExtract_ParenFormRoutes(t *testing.T) {
	r := frameworks.NewPhoenixResolver(t.TempDir())
	src := `defmodule PlausibleWeb.Router do
  use PlausibleWeb, :router

  scope "/", PlausibleWeb do
    get("/openapi", PageController, :openapi)
    get("/shared_links", SharedLinkController, :index)
    post("/x", SomeController, :create)
  end
end
`
	nodes, refs := r.Extract("lib/plausible_web/router.ex", src)

	if len(nodes) != 3 {
		t.Fatalf("expected 3 route nodes from paren-form routes, got %d: %v", len(nodes), elixirNodeNames(nodes))
	}

	// Build lookup maps.
	nodesByName := make(map[string]bool, len(nodes))
	for _, n := range nodes {
		nodesByName[n.Name] = true
	}
	refsByName := make(map[string]bool, len(refs))
	for _, ref := range refs {
		refsByName[ref.ReferenceName] = true
	}

	wantRoutes := []string{"GET /openapi", "GET /shared_links", "POST /x"}
	for _, want := range wantRoutes {
		if !nodesByName[want] {
			t.Errorf("paren-form route %q not extracted; got: %v", want, elixirNodeNames(nodes))
		}
	}

	wantRefs := []string{"openapi", "index", "create"}
	for _, want := range wantRefs {
		if !refsByName[want] {
			t.Errorf("expected ref %q from paren-form route; got: %v", want, elixirRefNames(refs))
		}
	}

	// All nodes and refs must carry LanguageElixir.
	for _, n := range nodes {
		if n.Language != types.LanguageElixir {
			t.Errorf("paren-form node %q language = %v, want LanguageElixir", n.Name, n.Language)
		}
	}
	for _, ref := range refs {
		if ref.Language != types.LanguageElixir {
			t.Errorf("paren-form ref %q language = %v, want LanguageElixir", ref.ReferenceName, ref.Language)
		}
	}
}

// TestPhoenixLanguages_ElixirUngated proves that Languages() returns
// LanguageElixir so getApplicableResolvers(LanguageElixir) includes
// PhoenixResolver. This is the contract getApplicableResolvers relies on.
func TestPhoenixLanguages_ElixirUngated(t *testing.T) {
	r := frameworks.NewPhoenixResolver(t.TempDir())
	langs := r.Languages()
	if len(langs) == 0 {
		t.Fatal("Languages() returned empty slice")
	}
	found := false
	for _, l := range langs {
		if l == types.LanguageElixir {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Languages() = %v; want to contain LanguageElixir so the pipeline includes this resolver on .ex files", langs)
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

// elixirNodeNames is a test helper that returns node Name fields.
func elixirNodeNames(nodes []types.Node) []string {
	names := make([]string, len(nodes))
	for i, n := range nodes {
		names[i] = n.Name
	}
	return names
}

// elixirRefNames is a test helper that returns ref ReferenceName fields.
func elixirRefNames(refs []types.UnresolvedReference) []string {
	names := make([]string, len(refs))
	for i, r := range refs {
		names[i] = r.ReferenceName
	}
	return names
}
