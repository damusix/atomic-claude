package languages

// Node-type strings are read from the live Elixir grammar — do not guess.
//
// Every Elixir construct parses as a "call" node, definitions included: they are
// macros, structurally identical to an ordinary call, and only the text of the
// "target" identifier tells them apart. That one fact shapes this whole config.
// Everything routes through StructTypes so ResolveKind can fan a single node
// type out to module, function, struct, import, or — via the "" sentinel — a
// plain call reference. The same guard is what keeps body scanning honest:
// visitFunctionBody skips any "call" whose ResolveKind is non-empty, since the
// declaration walk already extracted it.
//
// The macro's arguments are named child 1, and what sits at arguments[0] varies
// per macro: an alias for defmodule and the directives, an inner call for def
// and its kin, a list for defstruct.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// elixirTargetText returns a call node's "target" text, or "" when it is absent
// or not an identifier — a dot call such as User.new has a "dot" target, so it
// can never be a definition macro.
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
		return ""
	}
	ts, _ := target.StartByte(ctx)
	te, _ := target.EndByte(ctx)
	if int(te) > len(src) {
		return ""
	}
	return src[ts:te]
}

// elixirArgsFirstChild returns arguments[0] of a call node, and whether it
// exists.
func elixirArgsFirstChild(ctx context.Context, node sitter.Node) (sitter.Node, bool) {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil || cnt < 2 {
		return sitter.Node{}, false
	}
	argsNode, err := node.NamedChild(ctx, 1)
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

// elixirUnwrapGuard reaches the function head inside a guard clause. "def foo(x)
// when guard" puts a binary_operator at arguments[0], and the head — a call, or
// a bare identifier when the function takes no arguments — is its first child.
// Reports false for anything else, leaving the node to the caller.
func elixirUnwrapGuard(ctx context.Context, node sitter.Node) (sitter.Node, bool) {
	kind, _ := node.Kind(ctx)
	if kind != "binary_operator" {
		return node, false
	}
	cnt, err := node.NamedChildCount(ctx)
	if err != nil || cnt == 0 {
		return node, false
	}
	head, err := node.NamedChild(ctx, 0)
	if err != nil {
		return node, false
	}
	isNull, _ := head.IsNull(ctx)
	if isNull {
		return node, false
	}
	return head, true
}

// elixirGetName reads the name out of a call node's arguments, which is why the
// name is not left to nameFromNode: ResolveBody has to return the original call
// node so the walk still reaches the do_block, and the name is not on that node.
// An unrecognized macro or a plain call returns "" and falls through.
func elixirGetName(ctx context.Context, node sitter.Node, src string) string {
	macro := elixirTargetText(ctx, node, src)
	switch macro {
	case "defmodule", "alias", "import", "use":
		if first, ok := elixirArgsFirstChild(ctx, node); ok {
			ts, _ := first.StartByte(ctx)
			te, _ := first.EndByte(ctx)
			if int(te) <= len(src) {
				return strings.TrimSpace(src[ts:te])
			}
		}
		return macro
	case "def", "defp", "defmacro":
		// The name is the target of the inner call at arguments[0]. Unwrapping
		// any guard first puts guarded and plain defs on one path.
		if first, ok := elixirArgsFirstChild(ctx, node); ok {
			head, headOk := elixirUnwrapGuard(ctx, first)
			if !headOk {
				head = first
			}
			kind, _ := head.Kind(ctx)
			if kind == "call" {
				return elixirTargetText(ctx, head, src)
			}
			// A function taking no arguments has a bare identifier for a head.
			if kind == "identifier" {
				ts, _ := head.StartByte(ctx)
				te, _ := head.EndByte(ctx)
				if int(te) <= len(src) {
					return strings.TrimSpace(src[ts:te])
				}
			}
		}
		return macro
	case "defstruct":
		// A module holds at most one, so the fixed name still qualifies
		// uniquely as "MyApp.Foo::defstruct".
		return "defstruct"
	}
	return ""
}

// ElixirExtractor returns the LanguageExtractor config for Elixir source files (.ex, .exs).
func ElixirExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// StructTypes is the only arm that runs ResolveKind, and ResolveKind is
		// what gives one node type its several meanings; see the file header.
		StructTypes: extraction.TypeSet("call"),

		// Lets the walk descend into the do-block of a non-definition macro
		// call, so a def nested inside "if ... do ... end" is still found. Only
		// do_block belongs here: an arguments child is already walked by the
		// call-reference path, and listing it would walk it twice.
		MacroDoBlockTypes: extraction.TypeSet("do_block"),

		NameField: "",

		GetName: elixirGetName,

		CallTypes: extraction.TypeSet("call"),

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
				// Sentinel: emit as a call reference.
				return types.NodeKind("")
			}
		},

		// Deliberately identity. The resolved node is what the walk descends
		// into, and only the original call node still has the do_block holding
		// the nested definitions; arguments[0] would lose them. GetName is what
		// makes that affordable, supplying the name this node lacks.
		ResolveBody: func(ctx context.Context, node sitter.Node, src string) (sitter.Node, error) {
			return node, nil
		},

		// Export status follows the macro keyword, not the name, so this cannot
		// be IsExportedByName. The node here is the original call.
		IsExported: func(ctx context.Context, node sitter.Node, src string) bool {
			return elixirTargetText(ctx, node, src) != "defp"
		},

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
