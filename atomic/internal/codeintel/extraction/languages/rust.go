package languages

// Rust language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-lang-details and tmp/probe19):
//
//   Top-level (direct children of source_file):
//     use_declaration     — use std::collections::HashMap;
//     struct_item         — pub struct Point { ... }  /  struct Internal { ... }
//     enum_item           — pub enum Direction { ... }
//     trait_item          — pub trait Shape { ... }  → NodeKindInterface via ResolveKind
//     impl_item           — impl Shape for Point { ... } (methods inside → function_item)
//     function_item       — pub fn main() { ... }  /  fn helper() { ... }
//     macro_invocation    — println!("…"), vec![…]  → call UnresolvedReference
//
//   Named iterator also sees (inside bodies):
//     function_item       — functions inside impl blocks
//     function_signature_item — trait method signatures (abstract, no body)
//     call_expression     — "real" function calls  (compute(x, y))
//     macro_invocation    — macro calls at any nesting level
//
// Type-kind disambiguation (ResolveKind):
//   - struct_item         → NodeKindStruct (default)
//   - enum_item           → NodeKindEnum
//   - trait_item          → NodeKindInterface  (Rust traits are the semantic interface)
//   impl_item and function_item are NOT in StructTypes; the extractor descends
//   into impl_item naturally (not matched → descend), finding function_item children.
//
// IsExported: a node has a "visibility_modifier" named child containing "pub".
// Checked via AST (preferred over text scan when the child exists).
//
// Field names:
//   - struct_item: name field = "name"
//   - enum_item:   name field = "name"
//   - trait_item:  name field = "name"
//   - function_item: name field = "name"
//   - use_declaration: no "name" field; ExtractImport uses source text.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// RustExtractor returns the LanguageExtractor config for Rust source files (.rs).
//
// Node-type strings are verified by parsing real Rust via the wazero binding
// (see tmp/probe-lang-details/main.go and tmp/probe19/main.go).
func RustExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// function_item covers all named functions (top-level and inside impl blocks).
		FunctionTypes: extraction.TypeSet("function_item"),

		// struct_item, enum_item, and trait_item are all wired through StructTypes
		// so that ResolveKind can dispatch to the correct semantic kind.
		//
		// Why StructTypes for trait_item? The LanguageExtractor's ResolveKind hook
		// is only called for nodes that match StructTypes. Using StructTypes for all
		// three Rust aggregate types allows a single ResolveKind hook to distinguish
		// them, mirroring the Go pattern (type_declaration → struct/interface/alias).
		StructTypes: extraction.TypeSet("struct_item", "enum_item", "trait_item"),

		// use_declaration covers all "use" import paths.
		ImportTypes: extraction.TypeSet("use_declaration"),

		// call_expression covers regular function and method calls.
		// macro_invocation covers macro calls (println!, vec!, assert!, etc.).
		// Both emit UnresolvedReference with EdgeKindCalls.
		CallTypes: extraction.TypeSet("call_expression", "macro_invocation"),

		// Field names in the Rust grammar.
		NameField:   "name",
		BodyField:   "body",
		ParamsField: "parameters",
		ReturnField: "return_type",

		// ResolveKind disambiguates struct_item / enum_item / trait_item:
		//   struct_item → NodeKindStruct
		//   enum_item   → NodeKindEnum
		//   trait_item  → NodeKindInterface
		ResolveKind: rustResolveKind,

		// GetSignature extracts the signature text for functions.
		GetSignature: rustGetSignature,

		// IsExported: checks for a "visibility_modifier" child that contains "pub".
		// Uses AST child inspection (not text scan) — more reliable and idiomatic.
		IsExported: rustIsExported,

		// ExtractImport extracts the use path from use_declaration nodes.
		ExtractImport: rustExtractImport,
	}
}

// rustResolveKind returns the correct semantic NodeKind for a struct_item,
// enum_item, or trait_item node:
//
//	struct_item → NodeKindStruct
//	enum_item   → NodeKindEnum   (dispatched via the EnumTypes path after ResolveKind)
//	trait_item  → NodeKindInterface
func rustResolveKind(ctx context.Context, node sitter.Node, _ string) types.NodeKind {
	// Use the caller-provided ctx so cancellation and deadlines propagate correctly.
	kind, err := node.Kind(ctx)
	if err != nil {
		return types.NodeKindStruct
	}
	switch kind {
	case "trait_item":
		return types.NodeKindInterface
	case "enum_item":
		// Return NodeKindEnum so the engine routes through extractEnum.
		return types.NodeKindEnum
	default:
		// struct_item → NodeKindStruct
		return types.NodeKindStruct
	}
}

// rustGetSignature returns the signature text for function_item nodes (everything
// before the body block).
func rustGetSignature(ctx context.Context, node sitter.Node, source string) string {
	kind, err := node.Kind(ctx)
	if err != nil {
		return ""
	}
	if kind != "function_item" {
		return ""
	}
	sb, err := node.StartByte(ctx)
	if err != nil {
		return ""
	}
	bodyNode, err := node.ChildByFieldName(ctx, "body")
	if err != nil {
		return ""
	}
	isNull, _ := bodyNode.IsNull(ctx)
	if isNull {
		eb, _ := node.EndByte(ctx)
		t := strings.TrimSpace(source[sb:eb])
		if len(t) > 200 {
			t = t[:200]
		}
		return t
	}
	bodySB, err := bodyNode.StartByte(ctx)
	if err != nil || bodySB <= sb {
		return ""
	}
	sig := strings.TrimSpace(source[sb:bodySB])
	if len(sig) > 200 {
		sig = sig[:200]
	}
	return sig
}

// rustIsExported reports whether a Rust node is exported (pub).
// It checks whether the node has a "visibility_modifier" named child whose text
// starts with "pub". This is an AST-based check — more reliable than text scan
// because visibility_modifier is a dedicated grammar node.
func rustIsExported(ctx context.Context, node sitter.Node, source string) bool {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return false
	}
	for i := uint64(0); i < cnt; i++ {
		ch, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		kind, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		if kind != "visibility_modifier" {
			continue
		}
		sb, _ := ch.StartByte(ctx)
		eb, _ := ch.EndByte(ctx)
		if int(eb) <= len(source) {
			text := source[sb:eb]
			if strings.HasPrefix(text, "pub") {
				return true
			}
		}
	}
	return false
}

// rustExtractImport extracts the use path from a use_declaration node.
//
//	use std::collections::HashMap;    → name="HashMap", path="std::collections::HashMap"
//	use std::fmt::{self, Display};    → name="fmt", path="std::fmt"
func rustExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "use_declaration" {
		return "", ""
	}
	sb, _ := node.StartByte(ctx)
	eb, _ := node.EndByte(ctx)
	if int(eb) > len(source) {
		return "", ""
	}
	text := strings.TrimSpace(source[sb:eb])

	// Strip "use " prefix and trailing ";".
	text = strings.TrimPrefix(text, "use ")
	text = strings.TrimSuffix(text, ";")
	text = strings.TrimSpace(text)

	if text == "" {
		return "", ""
	}

	// Handle grouped imports: "std::fmt::{self, Display}" → use "std::fmt" as path.
	if idx := strings.Index(text, "::{"); idx >= 0 {
		path = text[:idx]
	} else {
		path = text
	}

	// Name = last segment after "::".
	segments := strings.Split(path, "::")
	name = segments[len(segments)-1]

	return name, path
}
