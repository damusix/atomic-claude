package languages_test

// Every fixture here runs through the real grammar, so these also cover ABI and
// pool wiring, not only the config.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// One fixture covering every macro the config recognizes, plus plain calls.
const elixirFixture = `defmodule MyApp.UserController do
  alias MyApp.User
  import Plug.Conn
  use Phoenix.Controller

  defstruct [:id, :name, :email]

  def create(conn, params) do
    user = User.new(params)
    json(conn, user)
  end

  def index(conn, _params) do
    users = User.all()
    json(conn, users)
  end

  defp validate(params) do
    params
  end
end
`

const elixirFixturePath = "lib/my_app/user_controller.ex"

// Modules are the containers everything else in Elixir hangs off.
func TestElixir_ModuleExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageElixir)
	if !ok {
		t.Fatal("Elixir not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), elixirFixturePath, elixirFixture, types.LanguageElixir)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	mod := findNode(result.Nodes, types.NodeKindModule, "MyApp.UserController")
	if mod == nil {
		t.Fatalf("MyApp.UserController module not found as NodeKindModule; nodes: %s", nodeKindList(result.Nodes))
	}
}

// Public functions are the call targets resolution and route wiring look for.
func TestElixir_PublicFunctionExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageElixir)
	if !ok {
		t.Fatal("Elixir not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), elixirFixturePath, elixirFixture, types.LanguageElixir)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "create")
	if fn == nil {
		t.Fatalf("create function not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if !fn.IsExported {
		t.Errorf("create should be exported (def); got IsExported=false")
	}
}

// A route resolver must not offer a private function as a routable action.
func TestElixir_PrivateFunctionExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageElixir)
	if !ok {
		t.Fatal("Elixir not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), elixirFixturePath, elixirFixture, types.LanguageElixir)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	fn := findNode(result.Nodes, types.NodeKindFunction, "validate")
	if fn == nil {
		t.Fatalf("validate function not found; nodes: %s", nodeKindList(result.Nodes))
	}
	if fn.IsExported {
		t.Errorf("validate should not be exported (defp); got IsExported=true")
	}
}

// Structs are Elixir's data types, and type-reference resolution needs them.
func TestElixir_StructExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageElixir)
	if !ok {
		t.Fatal("Elixir not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), elixirFixturePath, elixirFixture, types.LanguageElixir)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	var structNode *types.Node
	for i := range result.Nodes {
		if result.Nodes[i].Kind == types.NodeKindStruct {
			structNode = &result.Nodes[i]
			break
		}
	}
	if structNode == nil {
		t.Fatalf("defstruct node not found as NodeKindStruct; nodes: %s", nodeKindList(result.Nodes))
	}
}

// All three directives are dependencies; without them modules sit unconnected.
func TestElixir_ImportsExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageElixir)
	if !ok {
		t.Fatal("Elixir not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), elixirFixturePath, elixirFixture, types.LanguageElixir)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	importNode := findNode(result.Nodes, types.NodeKindImport, "MyApp.User")
	if importNode == nil {
		t.Fatalf("MyApp.User import not found; nodes: %s", nodeKindList(result.Nodes))
	}
}

// Extraction emits references, never edges: the resolution layer owns that step,
// and an edge minted here would bypass it unresolvable.
func TestElixir_CallEmitsUnresolvedReference(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageElixir)
	if !ok {
		t.Fatal("Elixir not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), elixirFixturePath, elixirFixture, types.LanguageElixir)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no EdgeKindCalls UnresolvedReferences; fixture has User.new(params), json(conn, user), User.all(); refs: %v", result.UnresolvedReferences)
	}
}

// Incremental indexing relies on a fixture extracting the same way twice.
func TestElixir_NodeCountStable(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageElixir)
	if !ok {
		t.Fatal("Elixir not registered")
	}
	e := newExtractor(t, extLang, cfg)

	r1 := e.Extract(context.Background(), elixirFixturePath, elixirFixture, types.LanguageElixir)
	r2 := e.Extract(context.Background(), elixirFixturePath, elixirFixture, types.LanguageElixir)

	if len(r1.Nodes) != len(r2.Nodes) {
		t.Errorf("node count not stable: %d vs %d", len(r1.Nodes), len(r2.Nodes))
	}
	if len(r1.UnresolvedReferences) != len(r2.UnresolvedReferences) {
		t.Errorf("unresolved ref count not stable: %d vs %d", len(r1.UnresolvedReferences), len(r2.UnresolvedReferences))
	}
}

// The blunt gate: a broken grammar load or broken wiring yields zero nodes
// rather than an error, and every other test here would still look plausible.
func TestElixir_NonZeroExtraction(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageElixir)
	if !ok {
		t.Fatal("Elixir not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), elixirFixturePath, elixirFixture, types.LanguageElixir)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// File, module, three functions, struct, and three imports.
	if len(result.Nodes) < 9 {
		t.Errorf("expected >= 9 nodes (file+module+functions+struct+imports), got %d; nodes: %s",
			len(result.Nodes), nodeKindList(result.Nodes))
	}
}

// Guard clauses, whose arguments[0] is a binary_operator rather than the call
// node the name extractor expects.
const elixirGuardFixture = `defmodule MyApp.Teams do
  def get(team_id) when is_integer(team_id) do
    {:ok, team_id}
  end

  defp validate_id(id) when is_integer(id) and id > 0 do
    :ok
  end

  def fetch_all when true do
    []
  end
end
`

// Regression guard: unwrapping the guard was once missing, so the name fell
// through to the macro keyword — hundreds of nodes in one real app were named
// "def" or "defp".
func TestElixir_GuardClause_RealNameExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageElixir)
	if !ok {
		t.Fatal("Elixir not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), "lib/my_app/teams.ex", elixirGuardFixture, types.LanguageElixir)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	for _, wantName := range []string{"get", "validate_id", "fetch_all"} {
		fn := findNode(result.Nodes, types.NodeKindFunction, wantName)
		if fn == nil {
			t.Errorf("guard-clause function %q not found; nodes: %s", wantName, nodeKindList(result.Nodes))
		}
	}

	// A function named for its own macro keyword is the bug's signature.
	for i := range result.Nodes {
		n := &result.Nodes[i]
		if n.Kind == types.NodeKindFunction && (n.Name == "def" || n.Name == "defp") {
			t.Errorf("function node has macro-keyword name %q (guard-clause bug); all nodes: %s",
				n.Name, nodeKindList(result.Nodes))
		}
	}
}

// Definitions nested in the do-block of a macro that is itself only a call.
const elixirMacroDoBlockFixture = `defmodule MyApp.StatsController do
  on_ee do
    def exploration_next(conn, params) do
      {:ok, params}
    end

    defp funnel(conn, params) do
      {:ok, conn}
    end
  end

  def index(conn, _params) do
    :ok
  end
end
`

// Regression guard: a call resolves to the empty sentinel, and the walk once
// stopped there without descending, so every definition inside such a block was
// absent from the index entirely.
func TestElixir_MacroDoBlock_DefsExtracted(t *testing.T) {
	t.Parallel()
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageElixir)
	if !ok {
		t.Fatal("Elixir not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), "lib/my_app/stats_controller.ex", elixirMacroDoBlockFixture, types.LanguageElixir)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	explorationNext := findNode(result.Nodes, types.NodeKindFunction, "exploration_next")
	if explorationNext == nil {
		t.Errorf("exploration_next (def inside on_ee do-block) not found; nodes: %s", nodeKindList(result.Nodes))
	} else if !explorationNext.IsExported {
		t.Errorf("exploration_next should be exported (def); got IsExported=false")
	}

	funnel := findNode(result.Nodes, types.NodeKindFunction, "funnel")
	if funnel == nil {
		t.Errorf("funnel (defp inside on_ee do-block) not found; nodes: %s", nodeKindList(result.Nodes))
	} else if funnel.IsExported {
		t.Errorf("funnel should NOT be exported (defp); got IsExported=true")
	}

	// Descending into the block must not cost the ordinary path.
	index := findNode(result.Nodes, types.NodeKindFunction, "index")
	if index == nil {
		t.Errorf("index (top-level def) not found; nodes: %s", nodeKindList(result.Nodes))
	}
}
