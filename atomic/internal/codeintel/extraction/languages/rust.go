package languages

// Node-type strings are read from the live Rust grammar — do not guess.
//
// impl_item is deliberately unwired: leaving it unmatched makes the walk descend
// into it, which is how the function_item nodes inside an impl block are found.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// RustExtractor returns the LanguageExtractor config for Rust source files (.rs).
func RustExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		FunctionTypes: extraction.TypeSet("function_item"),

		// All three aggregate types go through StructTypes because that is the
		// only arm that runs ResolveKind, which is what tells them apart.
		StructTypes: extraction.TypeSet("struct_item", "enum_item", "trait_item"),

		ImportTypes: extraction.TypeSet("use_declaration"),

		// Macro invocations count as calls.
		CallTypes: extraction.TypeSet("call_expression", "macro_invocation"),

		NameField:   "name",
		BodyField:   "body",
		ParamsField: "parameters",
		ReturnField: "return_type",

		ResolveKind: rustResolveKind,

		GetSignature: rustGetSignature,

		IsExported: rustIsExported,

		ExtractImport: rustExtractImport,
	}
}

// rustResolveKind maps each aggregate-type node to its own kind. A trait is
// Rust's interface.
func rustResolveKind(ctx context.Context, node sitter.Node, _ string) types.NodeKind {
	kind, err := node.Kind(ctx)
	if err != nil {
		return types.NodeKindStruct
	}
	switch kind {
	case "trait_item":
		return types.NodeKindInterface
	case "enum_item":
		return types.NodeKindEnum
	default:
		return types.NodeKindStruct
	}
}

// rustGetSignature returns everything before the body, truncated to 200 bytes.
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

// rustIsExported looks for a visibility_modifier child starting with "pub".
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

// rustExtractImport returns the use path and its last segment as the name. A
// grouped import yields the module it groups under: "std::fmt::{self, Display}"
// → "std::fmt".
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

	text = strings.TrimPrefix(text, "use ")
	text = strings.TrimSuffix(text, ";")
	text = strings.TrimSpace(text)

	if text == "" {
		return "", ""
	}

	if idx := strings.Index(text, "::{"); idx >= 0 {
		path = text[:idx]
	} else {
		path = text
	}

	segments := strings.Split(path, "::")
	name = segments[len(segments)-1]

	return name, path
}
