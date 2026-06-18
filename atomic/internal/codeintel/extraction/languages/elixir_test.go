package languages_test

// Tests for the Elixir language extractor config (CP2: engine wiring + extractor).
//
// Each test:
//  1. Extracts a real fixture through the pool (proves grammar ABI + wiring ok).
//  2. Asserts per success criteria:
//     - defmodule → NodeKindModule (module name extracted from alias)
//     - def       → NodeKindFunction, IsExported=true
//     - defp      → NodeKindFunction, IsExported=false
//     - defstruct → NodeKindStruct
//     - alias/import/use → NodeKindImport (UnresolvedReference with EdgeKindImports)
//     - regular calls → UnresolvedReference with EdgeKindCalls
//     - Node count stable across two extractions.
//
// Node-type strings are VERIFIED by real Elixir grammar parse (see
// tmp/probe-elixir/ for probe output). Do NOT change them without re-running
// the probe — all Elixir constructs parse as "call" nodes.

import (
	"context"
	"testing"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction/languages"
)

// elixirFixture exercises:
//   - defmodule (MyApp.UserController)  → NodeKindModule
//   - alias (MyApp.User)                → NodeKindImport
//   - import (Plug.Conn)                → NodeKindImport
//   - use (Phoenix.Controller)          → NodeKindImport
//   - defstruct ([:id, :name, :email])  → NodeKindStruct
//   - def create(conn, params)          → NodeKindFunction, IsExported=true
//   - def index(conn, _params)          → NodeKindFunction, IsExported=true
//   - defp validate(params)             → NodeKindFunction, IsExported=false
//   - regular call User.new(params)     → UnresolvedReference EdgeKindCalls
//   - regular call json(conn, user)     → UnresolvedReference EdgeKindCalls
//
// Verified by tmp/probe-elixir/: all Elixir definitions parse as "call" nodes
// with a "target" identifier child whose text is the macro name.
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

// TestElixir_ModuleExtracted verifies defmodule → NodeKindModule with the
// correct module name extracted from the alias node.
// WHY: Module nodes are the structural containers for Elixir code; missing
// them breaks the entire module-level graph.
func TestElixir_ModuleExtracted(t *testing.T) {
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

// TestElixir_PublicFunctionExtracted verifies def → NodeKindFunction with IsExported=true.
// WHY: Public functions are the primary call targets in Elixir; wrong kind or
// wrong export flag breaks call-graph resolution and Phoenix route wiring.
func TestElixir_PublicFunctionExtracted(t *testing.T) {
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

// TestElixir_PrivateFunctionExtracted verifies defp → NodeKindFunction with IsExported=false.
// WHY: Private functions must be distinguished from public ones; a Phoenix route
// resolver must not expose defp functions as routable actions.
func TestElixir_PrivateFunctionExtracted(t *testing.T) {
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

// TestElixir_StructExtracted verifies defstruct → NodeKindStruct.
// WHY: Elixir structs are the primary data types; their extraction enables
// type-reference resolution and data-flow analysis.
func TestElixir_StructExtracted(t *testing.T) {
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

// TestElixir_ImportsExtracted verifies alias/import/use → NodeKindImport.
// WHY: Import nodes enable dependency tracking between Elixir modules; without
// them the graph has no edges between the user controller and MyApp.User.
func TestElixir_ImportsExtracted(t *testing.T) {
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

// TestElixir_CallEmitsUnresolvedReference verifies regular call nodes emit
// EdgeKindCalls UnresolvedReferences (not edges directly).
// WHY: Calls must never emit edges directly — the resolution layer owns that
// step. A direct edge at extraction time bypasses resolution and produces an
// incorrect, unresolvable graph entry.
func TestElixir_CallEmitsUnresolvedReference(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageElixir)
	if !ok {
		t.Fatal("Elixir not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), elixirFixturePath, elixirFixture, types.LanguageElixir)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// Fixture has regular calls: User.new(params), json(conn, user), User.all().
	// These should emit EdgeKindCalls UnresolvedReferences, NOT NodeKindFunction nodes.
	callRefs := countUnresolved(result.UnresolvedReferences, types.EdgeKindCalls)
	if callRefs == 0 {
		t.Fatalf("no EdgeKindCalls UnresolvedReferences; fixture has User.new(params), json(conn, user), User.all(); refs: %v", result.UnresolvedReferences)
	}
}

// TestElixir_NodeCountStable verifies extraction is deterministic (idempotent).
// WHY: Non-deterministic extraction (e.g. from pointer aliasing or global state)
// causes flaky test failures and unreliable incremental indexing.
func TestElixir_NodeCountStable(t *testing.T) {
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

// TestElixir_NonZeroExtraction verifies that a real .ex file produces > 0 nodes.
// WHY: This is the primary regression gate — if Elixir extraction silently
// regresses to 0 nodes (e.g. grammar not loaded, wiring broken), this test
// fails immediately.
func TestElixir_NonZeroExtraction(t *testing.T) {
	cfg, extLang, ok := languages.NewRegistry().For(types.LanguageElixir)
	if !ok {
		t.Fatal("Elixir not registered")
	}
	e := newExtractor(t, extLang, cfg)
	result := e.Extract(context.Background(), elixirFixturePath, elixirFixture, types.LanguageElixir)
	if len(result.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", result.Errors)
	}

	// Must extract: 1 file node + 1 module + 2 public functions + 1 private function
	// + 1 struct + at least 3 import nodes = minimum 9 nodes.
	if len(result.Nodes) < 9 {
		t.Errorf("expected >= 9 nodes (file+module+functions+struct+imports), got %d; nodes: %s",
			len(result.Nodes), nodeKindList(result.Nodes))
	}
}
