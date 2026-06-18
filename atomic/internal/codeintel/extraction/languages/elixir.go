package languages

// Elixir language extractor configuration.
//
// Verified node-type strings (probed via tmp/probe-elixir/ — Elixir grammar
// tree-sitter-elixir v0.3.5, ABI 14):
//
// All Elixir constructs parse as "call" nodes. The macro name lives in the
// node's "target" field (an identifier child). The second named child
// (index 1) is an "arguments" node. Definitions are macros — structurally
// identical to regular function calls; only the target identifier text
// distinguishes them.
//
//	call.target → identifier text:
//	  "defmodule" — module definition; module name in arguments[0] (alias node)
//	  "def"       — public function; function spec in arguments[0] (inner call node)
//	  "defp"      — private function; same structure as "def"
//	  "defstruct" — struct definition; fields in arguments[0] (list node)
//	  "defmacro"  — macro definition; treated as public function for graph purposes
//	  "alias"     — module alias directive; module in arguments[0] (alias node)
//	  "import"    — import directive; module in arguments[0] (alias node)
//	  "use"       — use directive; module in arguments[0] (alias node)
//	  anything else (dot target or unknown identifier) → regular call reference
//
// Name extraction:
//   - defmodule/alias/import/use: ResolveBody returns arguments[0] (alias leaf),
//     nameFromNode fallback returns full alias text (e.g. "MyApp.UserController").
//   - def/defp/defmacro: ResolveBody returns arguments[0] (inner call node),
//     nameFromNode fallback returns its first identifier child (the function name).
//   - defstruct: ResolveBody returns the outer call node; nameFromNode finds the
//     "defstruct" identifier as first named child → name = "defstruct". The
//     qualified name (e.g. "MyApp.Foo::defstruct") is unique within the module.
//
// IsExported rule:
//   - defmodule, defstruct, alias, import, use → true (public by nature)
//   - def, defmacro → true
//   - defp → false (private function)
//
// Edges:
//   - alias/import/use macro calls → emitted as NodeKindImport (via ResolveKind).
//   - Regular function calls (non-macro) → emitted as call UnresolvedReferences
//     (via the "" sentinel from ResolveKind).
//   - Nested calls inside def/defp bodies are captured by visitFunctionBody
//     because "call" is also in CallTypes. CallTypes IS set (to "call"); body-level
//     call extraction is active. Definition macros are filtered OUT inside
//     visitFunctionBody via the StructTypes+ResolveKind guard: when ResolveKind
//     returns a non-"" kind (e.g. NodeKindFunction for "def"), the node is a
//     definition already extracted by the visitChildren path and is skipped.
//     Only nodes where ResolveKind returns "" (regular function calls like
//     User.new/json/etc.) reach extractCall and emit an UnresolvedReference.
//
// NOTE: Because ALL "call" nodes go through StructTypes → ResolveKind, and
// ResolveKind returns "" (the call-ref sentinel) for non-definition calls,
// every non-macro call encountered at the file/module/function scope level emits
// an UnresolvedReference via extractCall. This covers alias/import/use as import
// nodes AND local/remote function calls at the top level of bodies. Body-level
// calls are also captured: visitFunctionBody checks CallTypes (set to "call"),
// and the StructTypes+ResolveKind guard filters definition macros so only genuine
// call references are emitted from body scanning.
//
// Probe files: tmp/probe-elixir/main.go, probe2.go, probe3.go, probe4.go

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// elixirTargetText returns the text of the "target" field of an Elixir call node.
// Returns "" if the target field is absent or is not an identifier (e.g. a dot
// call like User.new(…) has a "dot" target, not an identifier).
func elixirTargetText(ctx context.Context, node sitter.Node, src string) string {
	target, err := node.ChildByFieldName(ctx, "target")
	if err != nil {
		return ""
	}
	isNull, _ := target.IsNull(ctx)
	if isNull {
		return ""
	}
	kind, _ := target.Kind(ctx)
	if kind != "identifier" {
		return "" // dot call (Mod.fun) — not a definition macro
	}
	ts, _ := target.StartByte(ctx)
	te, _ := target.EndByte(ctx)
	if int(te) > len(src) {
		return ""
	}
	return src[ts:te]
}

// elixirArgsFirstChild returns the first named child of the arguments node
// (named child index 1 of the call node). Returns (zero, false) when absent.
func elixirArgsFirstChild(ctx context.Context, node sitter.Node) (sitter.Node, bool) {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil || cnt < 2 {
		return sitter.Node{}, false
	}
	argsNode, err := node.NamedChild(ctx, 1) // named child [1] = arguments wrapper
	if err != nil {
		return sitter.Node{}, false
	}
	isNull, _ := argsNode.IsNull(ctx)
	if isNull {
		return sitter.Node{}, false
	}
	argCnt, err := argsNode.NamedChildCount(ctx)
	if err != nil || argCnt == 0 {
		return sitter.Node{}, false
	}
	first, err := argsNode.NamedChild(ctx, 0)
	if err != nil {
		return sitter.Node{}, false
	}
	isNull, _ = first.IsNull(ctx)
	if isNull {
		return sitter.Node{}, false
	}
	return first, true
}

// elixirGetName extracts the symbol name from an Elixir call node without
// affecting body traversal. Used as the GetName hook so that ResolveBody can
// safely return the original call node (enabling visitChildren to descend into
// the do_block) while GetName independently extracts the correct name.
//
// Name strategy per macro:
//   - defmodule: args[0] is alias("MyApp.Foo") → full text = "MyApp.Foo"
//   - def/defp/defmacro: args[0] is inner call("bar(x)") → target text = "bar"
//   - defstruct: args[0] is list → name = "defstruct" (unique within module)
//   - alias/import/use: args[0] is alias("MyApp.User") → full text = "MyApp.User"
//   - anything else: returns "" (fall through to nameFromNode)
func elixirGetName(ctx context.Context, node sitter.Node, src string) string {
	macro := elixirTargetText(ctx, node, src)
	switch macro {
	case "defmodule", "alias", "import", "use":
		// Name = text of arguments[0] (alias/module-path node).
		if first, ok := elixirArgsFirstChild(ctx, node); ok {
			ts, _ := first.StartByte(ctx)
			te, _ := first.EndByte(ctx)
			if int(te) <= len(src) {
				return strings.TrimSpace(src[ts:te])
			}
		}
		return macro
	case "def", "defp", "defmacro":
		// Name = "target" text of the inner call node (arguments[0]).
		// The inner call is bar(x); its target field is identifier("bar").
		if first, ok := elixirArgsFirstChild(ctx, node); ok {
			kind, _ := first.Kind(ctx)
			if kind == "call" {
				return elixirTargetText(ctx, first, src)
			}
			// Guard: if the args structure is flat (no-arg function like `def foo`),
			// first child may itself be an identifier.
			if kind == "identifier" {
				ts, _ := first.StartByte(ctx)
				te, _ := first.EndByte(ctx)
				if int(te) <= len(src) {
					return strings.TrimSpace(src[ts:te])
				}
			}
		}
		return macro
	case "defstruct":
		// Elixir convention: there is exactly one defstruct per module. Use the
		// fixed name "defstruct" so the qualified name is "MyApp.Foo::defstruct".
		return "defstruct"
	}
	return "" // fall through to nameFromNode for unrecognised macros / regular calls
}

// ElixirExtractor returns the LanguageExtractor config for Elixir source files (.ex, .exs).
//
// Node-type strings are verified by parsing real Elixir via the wazero binding
// (see tmp/probe-elixir/ for exact output).
func ElixirExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// All Elixir constructs are "call" nodes. StructTypes is used because it
		// is the only dispatch path that supports ResolveKind to fan out a single
		// node kind to multiple semantic roles (module, function, struct, import,
		// or call reference). The ResolveKind hook reads the "target" identifier
		// text to decide which semantic role applies.
		StructTypes: extraction.TypeSet("call"),

		// NameField is intentionally empty. Name extraction is handled entirely
		// by the GetName hook — which reads the correct inner child per macro —
		// and by nameFromNode fallback when GetName returns "".
		NameField: "",

		// GetName extracts the symbol name from the original call node, independent
		// of which node ResolveBody returns. This separation is required because:
		//   - ResolveBody must return the original call node (so visitChildren
		//     descends into the do_block and finds nested def/defp/defstruct).
		//   - The name lives in a different child (arguments[0] or its sub-tree),
		//     not in the call node directly.
		// See elixirGetName for the per-macro strategy.
		GetName: elixirGetName,

		// CallTypes enables body-level call extraction in visitFunctionBody.
		// "call" is used here because ALL Elixir calls (regular and macro) share
		// this node kind. The definition macros (def, defmodule, etc.) are
		// filtered OUT in visitFunctionBody via the StructTypes+ResolveKind guard:
		// when a "call" node's ResolveKind returns a non-"" kind, it is a
		// definition — visitFunctionBody skips it (it was already extracted by
		// the visitChildren path). Only nodes where ResolveKind returns "" (i.e.
		// regular function calls like User.new/json/etc.) reach extractCall.
		CallTypes: extraction.TypeSet("call"),

		// ResolveKind inspects the "target" identifier text of a call node and
		// returns the appropriate NodeKind:
		//   defmodule → NodeKindModule
		//   def / defmacro → NodeKindFunction (IsExported=true)
		//   defp → NodeKindFunction (IsExported=false, via IsExported hook)
		//   defstruct → NodeKindStruct
		//   alias / import / use → NodeKindImport
		//   anything else → "" (empty sentinel = emit as call reference)
		ResolveKind: func(ctx context.Context, node sitter.Node, src string) types.NodeKind {
			switch elixirTargetText(ctx, node, src) {
			case "defmodule":
				return types.NodeKindModule
			case "def", "defmacro":
				return types.NodeKindFunction
			case "defp":
				return types.NodeKindFunction
			case "defstruct":
				return types.NodeKindStruct
			case "alias", "import", "use":
				return types.NodeKindImport
			default:
				return types.NodeKind("") // call reference sentinel
			}
		},

		// ResolveBody returns the ORIGINAL call node for all macros.
		//
		// Rationale: extractClass/extractFunction call BOTH GetName (for name) AND
		// visitChildren (for body traversal) on the resolved node. Returning the
		// original call node means visitChildren descends into all named children —
		// including the do_block — and finds nested def/defp/defstruct nodes.
		//
		// If we returned arguments[0] (the alias or inner call), visitChildren
		// would walk the alias node (no children) or inner call (its arguments),
		// missing the do_block entirely. GetName provides the correct name
		// independent of this decision.
		//
		// For the "" sentinel (regular calls), ResolveBody is never called because
		// the ResolveKind "" case dispatches directly to extractCall.
		ResolveBody: func(ctx context.Context, node sitter.Node, src string) (sitter.Node, error) {
			// Return original node unchanged. GetName handles name extraction;
			// visitChildren handles body traversal via do_block.
			return node, nil
		},

		// IsExported: defp functions are private (IsExported=false); everything
		// else is public. The hook receives the original call node (before
		// ResolveBody), so we read the target text here.
		//
		// IsExportedByName is not used because export status depends on the macro
		// keyword, not the symbol name.
		IsExported: func(ctx context.Context, node sitter.Node, src string) bool {
			return elixirTargetText(ctx, node, src) != "defp"
		},

		// ExtractImport extracts the module path from alias/import/use nodes.
		// The path is the text of arguments[0] (the alias node).
		ExtractImport: func(ctx context.Context, node sitter.Node, src string) (name string, path string) {
			if first, ok := elixirArgsFirstChild(ctx, node); ok {
				ts, _ := first.StartByte(ctx)
				te, _ := first.EndByte(ctx)
				if int(te) <= len(src) {
					path = strings.TrimSpace(src[ts:te])
					name = path
				}
			}
			if name == "" {
				macro := elixirTargetText(ctx, node, src)
				name = macro
			}
			return name, path
		},
	}
}
