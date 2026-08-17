package languages

// Node-type strings are read from the live C grammar — do not guess.
//
// Neither function_definition nor type_definition carries a "name" field, so
// this config leaves NameField empty and lets the name-from-node fallback find
// the identifier by scanning named children. C has no notion of export, so
// "exported" here means link-visible: anything not marked static.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// CExtractor returns the LanguageExtractor config for C source files (.c, .h).
func CExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		FunctionTypes: extraction.TypeSet("function_definition"),

		// typedef struct and typedef enum share this node type; ResolveKind
		// splits them. StructTypes is the arm that runs it.
		StructTypes: extraction.TypeSet("type_definition"),

		ImportTypes: extraction.TypeSet("preproc_include"),

		CallTypes: extraction.TypeSet("call_expression"),

		// Empty so the name-from-node fallback fires; see the file header.
		NameField: "",

		ResolveBody: cResolveBody,

		ResolveKind: cResolveKind,

		IsExported: cIsExported,

		ExtractImport: cExtractInclude,
	}
}

// cResolveBody unwraps function_definition to its function_declarator child,
// which is where the name-from-node fallback can reach the identifier. Any other
// node passes through unchanged.
func cResolveBody(ctx context.Context, node sitter.Node, source string) (sitter.Node, error) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return node, nil
	}
	if kind != "function_definition" {
		return node, nil
	}
	declNode, err := node.ChildByFieldName(ctx, "declarator")
	if err != nil {
		return node, nil
	}
	isNull, _ := declNode.IsNull(ctx)
	if isNull {
		return node, nil
	}
	return declNode, nil
}

// cResolveKind reads a type_definition's first named child: an enum_specifier
// makes it an enum, anything else a struct.
func cResolveKind(ctx context.Context, node sitter.Node, source string) types.NodeKind {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "type_definition" {
		return types.NodeKindStruct
	}
	cnt, err := node.NamedChildCount(ctx)
	if err != nil || cnt == 0 {
		return types.NodeKindStruct
	}
	firstChild, err := node.NamedChild(ctx, 0)
	if err != nil {
		return types.NodeKindStruct
	}
	firstKind, err := firstChild.Kind(ctx)
	if err != nil {
		return types.NodeKindStruct
	}
	if firstKind == "enum_specifier" {
		return types.NodeKindEnum
	}
	return types.NodeKindStruct
}

// cIsExported reports a function as exported unless it is static. Typedefs are
// always exported.
func cIsExported(ctx context.Context, node sitter.Node, source string) bool {
	kind, err := node.Kind(ctx)
	if err != nil {
		return false
	}
	if kind == "type_definition" {
		return true
	}
	if kind != "function_definition" {
		return true
	}
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return true
	}
	for i := uint64(0); i < cnt; i++ {
		ch, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		ck, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		if ck != "storage_class_specifier" {
			continue
		}
		sb, _ := ch.StartByte(ctx)
		eb, _ := ch.EndByte(ctx)
		if int(eb) <= len(source) {
			text := source[sb:eb]
			if strings.TrimSpace(text) == "static" {
				return false
			}
		}
	}
	return true
}

// cExtractInclude returns the header path and its last segment as the name.
// Angle-bracket and quoted forms are treated alike. Shared with C++.
func cExtractInclude(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "preproc_include" {
		return "", ""
	}

	cnt, _ := node.NamedChildCount(ctx)
	for i := uint64(0); i < cnt; i++ {
		ch, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		ck, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		if ck != "system_lib_string" && ck != "string_literal" {
			continue
		}
		sb, _ := ch.StartByte(ctx)
		eb, _ := ch.EndByte(ctx)
		if int(eb) > len(source) {
			continue
		}
		text := source[sb:eb]
		text = strings.Trim(text, "<>\"")
		if text == "" {
			continue
		}
		path = text
		segments := strings.Split(text, "/")
		name = segments[len(segments)-1]
		return name, path
	}
	return "", ""
}
