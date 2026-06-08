package languages

// C language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-cp8a/ — C grammar):
//
//	Top-level (direct children of translation_unit):
//	  preproc_include       — #include <stdio.h>
//	  type_definition       — typedef struct { ... } Point; / typedef enum { ... } Color;
//	  function_definition   — static int helper(...) / int add(...)
//
//	Named-iterator also sees (inside bodies):
//	  call_expression       — printf(...) / helper(p->x) / add(p.x, p.y)
//
// Name extraction for function_definition:
//
//	function_definition has no 'name' field. The name path is:
//	  function_definition → declarator (function_declarator) → first-named-child (identifier)
//	Solution: ResolveBody unwraps function_definition → function_declarator.
//	Then nameFromNode fallback finds 'identifier' as the first named child.
//	NameField = "" so the fallback always fires.
//
// Name extraction for type_definition (typedef struct/enum):
//
//	type_definition's last-named-child is the type_identifier (alias name).
//	With NameField = "", nameFromNode iterates named children:
//	  child[0]: struct_specifier / enum_specifier — no identifier suffix, skipped
//	  child[1]: type_identifier — matches HasSuffix "_identifier", extracted
//	The typedef alias (e.g. "Point") is correctly extracted.
//
// Kind disambiguation for type_definition (ResolveKind):
//
//	type_definition wraps either struct_specifier → NodeKindStruct
//	                         or enum_specifier   → NodeKindEnum
//	ResolveKind inspects the first named child's kind to decide.
//
// IsExported rule: C has no export keyword. Non-static top-level functions are
// visible at link time. Rule: absence of storage_class_specifier with text "static"
// means exported. Presence of storage_class_specifier "static" means not exported.
// Typedef types (type_definition) are always exported.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// CExtractor returns the LanguageExtractor config for C source files (.c, .h).
//
// Node-type strings are verified by parsing real C via the wazero binding
// (see tmp/probe-cp8a/main.go, probe2.go, probe3.go, probe4.go).
func CExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// function_definition covers all named functions.
		FunctionTypes: extraction.TypeSet("function_definition"),

		// type_definition covers typedef struct and typedef enum.
		// ResolveKind dispatches to NodeKindStruct vs NodeKindEnum.
		StructTypes: extraction.TypeSet("type_definition"),

		// preproc_include covers all #include directives.
		ImportTypes: extraction.TypeSet("preproc_include"),

		// call_expression covers all function/method calls.
		CallTypes: extraction.TypeSet("call_expression"),

		// NameField = "" — C function names live deep in the declarator chain.
		// nameFromNode fallback finds 'identifier' as first-named-child after
		// ResolveBody unwraps function_definition → function_declarator.
		// For type_definition, fallback finds 'type_identifier' (last named child).
		NameField: "",

		// ResolveBody unwraps function_definition to its function_declarator child,
		// so nameFromNode finds the identifier without a NameField.
		ResolveBody: cResolveBody,

		// ResolveKind distinguishes typedef struct (NodeKindStruct) from typedef enum
		// (NodeKindEnum) by inspecting the first named child of type_definition.
		ResolveKind: cResolveKind,

		// IsExported: non-static top-level functions are exported; static are not.
		// Type definitions (typedef struct/enum) are always exported.
		IsExported: cIsExported,

		// ExtractImport extracts the header path from preproc_include nodes.
		ExtractImport: cExtractInclude,
	}
}

// cResolveBody unwraps a function_definition node to its function_declarator child.
// This is needed because C function names live at:
//
//	function_definition → function_declarator → identifier (first named child)
//
// For non-function_definition nodes, returns the same node unchanged.
func cResolveBody(ctx context.Context, node sitter.Node, source string) (sitter.Node, error) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return node, nil
	}
	if kind != "function_definition" {
		return node, nil
	}
	// The 'declarator' field holds the function_declarator.
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

// cResolveKind returns NodeKindStruct for typedef struct and NodeKindEnum for
// typedef enum, by inspecting the first named child of a type_definition node.
//
//	type_definition → struct_specifier → NodeKindStruct
//	type_definition → enum_specifier   → NodeKindEnum
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

// cIsExported reports whether a C node is exported.
// Rule: top-level functions without a "static" storage_class_specifier are exported.
// Type definitions (typedef struct/enum) are always considered exported.
func cIsExported(ctx context.Context, node sitter.Node, source string) bool {
	kind, err := node.Kind(ctx)
	if err != nil {
		return false
	}
	// typedef types are always exported.
	if kind == "type_definition" {
		return true
	}
	// For function_definition: check for storage_class_specifier "static".
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

// cExtractInclude extracts the header path from a preproc_include node.
//
//	#include <stdio.h>    → name="stdio.h",  path="stdio.h"
//	#include "myfile.h"  → name="myfile.h", path="myfile.h"
func cExtractInclude(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "preproc_include" {
		return "", ""
	}

	// First named child is system_lib_string (<stdio.h>) or string_literal ("myfile.h").
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
		// Strip angle brackets or quotes.
		text = strings.Trim(text, "<>\"")
		if text == "" {
			continue
		}
		path = text
		// Name = last path segment (e.g. "stdio.h" from "sys/stdio.h").
		segments := strings.Split(text, "/")
		name = segments[len(segments)-1]
		return name, path
	}
	return "", ""
}
