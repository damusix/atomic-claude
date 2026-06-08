package languages

// Objective-C language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-cp8d/ — ObjC grammar):
//
//	Top-level (direct children of translation_unit):
//	  preproc_include            — "#import <Foundation/Foundation.h>" / "#import "Shape.h""
//	  protocol_declaration       — "@protocol Drawable <NSObject> ... @end"
//	  class_interface            — "@interface Shape : NSObject ... @end"
//	  class_implementation       — "@implementation Shape ... @end"
//	  function_definition        — "Shape *createShape(...) { ... }"
//
//	Inside class_interface / class_implementation:
//	  method_declaration         — "- (void)draw;"  (declaration, no body)
//	  implementation_definition  — wraps method_definition (concrete implementation)
//	    method_definition        — "- (instancetype)initWithId:... { ... }"
//	  property_declaration       — "@property (nonatomic, assign) NSInteger shapeId;"
//
//	Call sites:
//	  message_expression         — "[self renderWithId:_shapeId]" / "[s draw]"
//	  call_expression            — "NSLog(@"rendering %@", _name)"
//
// Name extraction:
//   - class_interface, class_implementation: first identifier named child = class name
//   - protocol_declaration: first identifier named child = protocol name
//   - method_declaration: first selector part identifier = selector name
//   - implementation_definition: wraps method_definition — ResolveBody unwraps it
//   - function_definition: first declarator identifier = function name
//
// IsExported rule: ObjC has no method-level access modifier. All symbols are
// public by default. IsExportedByName returns true unconditionally.
// (@private/@protected are ivar section markers, not method modifiers — out of scope.)
//
// ExtractImport handles both angle-bracket and quoted forms:
//
//	#import <Foundation/Foundation.h> → name="Foundation.h", path="Foundation/Foundation.h"
//	#import "Shape.h"                 → name="Shape.h",       path="Shape.h"

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ObjCExtractor returns the LanguageExtractor config for Objective-C source files (.m, .h).
//
// Node-type strings are verified by parsing real ObjC via the wazero binding
// (see tmp/probe-cp8d/main.go).
func ObjCExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// class_interface covers @interface declarations.
		// class_implementation covers @implementation blocks.
		ClassTypes: extraction.TypeSet("class_interface", "class_implementation"),

		// protocol_declaration covers @protocol ... @end blocks.
		// These are the ObjC semantic equivalent of interfaces.
		InterfaceTypes: extraction.TypeSet("protocol_declaration"),

		// function_definition covers C-style free functions.
		// implementation_definition covers method implementations inside @implementation
		// — they wrap a method_definition child, resolved by ResolveBody.
		FunctionTypes: extraction.TypeSet("function_definition", "implementation_definition"),

		// method_declaration covers method declarations in @interface and @protocol.
		// These are declaration-only (no body); the implementations live in
		// implementation_definition nodes in the @implementation section.
		MethodTypes: extraction.TypeSet("method_declaration"),

		// preproc_include covers both #import and #include directives.
		ImportTypes: extraction.TypeSet("preproc_include"),

		// message_expression covers ObjC message sends: [obj method], [obj method:arg].
		// call_expression covers C-style calls: NSLog(...), malloc(n).
		CallTypes: extraction.TypeSet("message_expression", "call_expression"),

		// No NameField: the grammar does not use a uniform "name" field.
		// nameFromNode's fallback identifier scan finds names correctly.
		NameField: "",

		// ResolveBody unwraps implementation_definition → method_definition before
		// name extraction. Without this, nameFromNode would look at the
		// implementation_definition node and find no usable identifier (its direct
		// named children are the inner method_definition, not an identifier).
		ResolveBody: objcResolveBody,

		// IsExportedByName: ObjC symbols are public by default.
		// Return true unconditionally.
		IsExportedByName: objcIsExportedByName,

		// ExtractImport: strip #import / #include prefix and angle brackets or quotes.
		ExtractImport: objcExtractImport,
	}
}

// objcResolveBody unwraps two node types to enable correct name extraction:
//
//  1. implementation_definition → method_definition
//     In @implementation blocks, each method impl is an implementation_definition
//     wrapping a method_definition child. We descend one level so nameFromNode
//     finds the selector identifier inside method_definition.
//
//  2. function_definition → identifier (via pointer_declarator → function_declarator)
//     C-style ObjC functions like "Shape *createShape(...)" have this structure:
//     function_definition
//     type_identifier   (return type, e.g. "Shape")
//     pointer_declarator
//     function_declarator
//     identifier    (function name, e.g. "createShape")
//     compound_statement (body)
//     nameFromNode picks type_identifier "Shape" first. We descend through
//     pointer_declarator → function_declarator and return the identifier child
//     so nameFromNode gets the correct name.
//
// For any other node type, the original node is returned unchanged.
func objcResolveBody(ctx context.Context, node sitter.Node, _ string) (sitter.Node, error) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return node, nil
	}

	switch kind {
	case "implementation_definition":
		// Unwrap to method_definition.
		cnt, err := node.NamedChildCount(ctx)
		if err != nil {
			return node, nil
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
			if ck == "method_definition" {
				return ch, nil
			}
		}

	case "function_definition":
		// Descend: pointer_declarator → function_declarator → identifier.
		ptrDecl, ok := firstNamedChildOfKind(ctx, node, "pointer_declarator")
		if !ok {
			// No pointer_declarator — try function_declarator directly.
			fnDecl, ok2 := firstNamedChildOfKind(ctx, node, "function_declarator")
			if !ok2 {
				return node, nil
			}
			ident, ok3 := firstNamedChildOfKind(ctx, fnDecl, "identifier")
			if !ok3 {
				return node, nil
			}
			return ident, nil
		}
		fnDecl, ok := firstNamedChildOfKind(ctx, ptrDecl, "function_declarator")
		if !ok {
			return node, nil
		}
		ident, ok := firstNamedChildOfKind(ctx, fnDecl, "identifier")
		if !ok {
			return node, nil
		}
		return ident, nil
	}

	return node, nil
}

// objcIsExportedByName reports that all ObjC symbols are exported (public).
//
// ObjC has no method-level access modifier. Symbols are public by default.
// @private and @protected are ivar section markers within class bodies —
// not per-method modifiers — and are out of scope for CP8.
func objcIsExportedByName(_ string) bool {
	return true
}

// objcExtractImport extracts the import path from a preproc_include node.
//
// ObjC grammar structure:
//
//	preproc_include
//	  "#import <Foundation/Foundation.h>"  ← full text of the directive
//	  "#import "Shape.h""
//
// We strip the directive keyword and delimiters to get the bare path.
//
//	#import <Foundation/Foundation.h> → path="Foundation/Foundation.h", name="Foundation.h"
//	#import "Shape.h"                 → path="Shape.h",                 name="Shape.h"
func objcExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	sb, _ := node.StartByte(ctx)
	eb, _ := node.EndByte(ctx)
	if int(eb) > len(source) {
		return "", ""
	}
	raw := strings.TrimSpace(source[sb:eb])
	// Strip directive prefix: "#import " or "#include "
	for _, prefix := range []string{"#import ", "#include "} {
		raw = strings.TrimPrefix(raw, prefix)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	// Strip surrounding delimiters: <...> or "..."
	if strings.HasPrefix(raw, "<") && strings.HasSuffix(raw, ">") {
		raw = raw[1 : len(raw)-1]
	} else if strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) {
		raw = raw[1 : len(raw)-1]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	path = raw
	// Name = last path segment (e.g. "Foundation/Foundation.h" → "Foundation.h").
	segments := strings.Split(path, "/")
	name = segments[len(segments)-1]
	return name, path
}

// Ensure objcIsExportedByName satisfies the IsExportedByName signature.
var _ func(string) bool = objcIsExportedByName

// Ensure objcExtractImport satisfies the ExtractImport signature.
var _ func(context.Context, sitter.Node, string) (string, string) = objcExtractImport

// Ensure objcResolveBody satisfies the ResolveBody signature.
var _ func(context.Context, sitter.Node, string) (sitter.Node, error) = objcResolveBody

// Ensure types package is used.
var _ = types.NodeKindFunction
