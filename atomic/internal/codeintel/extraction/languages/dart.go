package languages

// Dart language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-cp8d/ — Dart grammar):
//
//	Top-level (direct children of program):
//	  import_or_export       — "import 'dart:async';" / "import 'package:...';"
//	  enum_declaration       — "enum Direction { north, south, east, west }"
//	  mixin_declaration      — "mixin Drawable { ... }"
//	  class_definition       — "abstract class Shape ..." / "class Circle ..."
//	  function_signature     — "double computeArea(Shape s)"
//	  function_body          — paired with top-level function_signature
//
//	Inside class_body (via unmatched descent):
//	  declaration            — wraps function_signature for abstract method decls
//	  method_signature       — wraps function_signature for concrete methods
//	  constructor_signature  — "Shape(this.id, this.name)"
//	  annotation             — "@override" (unmatched, skipped)
//	  function_signature     — inside declaration/method_signature
//
// Name extraction:
//   - class_definition: fallback to first identifier child ("Shape", "Circle")
//   - enum_declaration: fallback to first identifier child ("Direction")
//   - mixin_declaration: fallback to first identifier child ("Drawable")
//   - function_signature: fallback to first identifier child (the function name)
//   - constructor_signature: fallback to first identifier child (class name)
//
// BLOCKED: Dart grammar has no call_expression node. Call sites appear as
// expression_statement containing identifier + selector; the engine's
// visitChildren walk has no call-node type to match against. This is a grammar
// constraint documented in TestDart_CallsBlocked. CallTypes is left empty.
//
// IsExported rule: Dart uses leading underscore for library-private symbols.
// No leading underscore = exported (public). Leading _ = not exported (private).
// Implemented via IsExportedByName (name-only predicate, no AST walk needed).

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// DartExtractor returns the LanguageExtractor config for Dart source files (.dart).
//
// Node-type strings are verified by parsing real Dart via the wazero binding
// (see tmp/probe-cp8d/main.go).
func DartExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// function_signature covers top-level functions and (via unmatched descent)
		// method declarations inside class_body. constructor_signature covers
		// constructors.
		FunctionTypes: extraction.TypeSet("function_signature", "constructor_signature"),

		// class_definition covers abstract and concrete classes.
		// mixin_declaration covers Dart mixins (semantic equivalent of a class/interface).
		ClassTypes: extraction.TypeSet("class_definition", "mixin_declaration"),

		// enum_declaration covers Dart enums.
		EnumTypes: extraction.TypeSet("enum_declaration"),

		// import_or_export covers both import and export directives.
		ImportTypes: extraction.TypeSet("import_or_export"),

		// CallTypes: intentionally empty — Dart grammar has no call_expression node.
		// See BLOCKED comment above and TestDart_CallsBlocked.

		// No NameField: the grammar does not use a "name" field name (probe confirmed).
		// The fallback identifier-scan in nameFromNode correctly finds names.
		NameField: "",

		// ResolveBody: for function_signature nodes, resolve to the identifier
		// child (the function name) to skip the return type. Without this,
		// nameFromNode's identifier fallback picks the return type (e.g. "double"
		// for "double computeArea(...)") instead of the function name.
		ResolveBody: dartResolveBody,

		// IsExportedByName: Dart's visibility convention is name-based.
		// Leading underscore = library-private (not exported).
		// No underscore = public (exported).
		IsExportedByName: dartIsExportedByName,

		// ExtractImport: Dart's import_or_export node wraps library_import which
		// wraps import_specification which contains a configurable_uri whose first
		// child is a uri whose first child is a string_literal with the path.
		ExtractImport: dartExtractImport,
	}
}

// dartResolveBody handles the return-type vs. function-name disambiguation
// for function_signature nodes in Dart.
//
// Dart grammar for function_signature:
//
//	[return_type (void_type | type_identifier | ...)]
//	[identifier — the function name]
//	[formal_parameter_list]
//
// nameFromNode's fallback scans named children for the first identifier or
// type_identifier. When the return type is a type_identifier (e.g. "double"),
// it is matched before the function-name identifier. To fix this, we walk
// the children and return the identifier child that immediately precedes
// formal_parameter_list — that is always the function name.
//
// For constructor_signature and any other node type, the node is returned
// unchanged (the fallback scan finds the constructor name correctly because
// constructor_signature has the class name as its first identifier child).
func dartResolveBody(ctx context.Context, node sitter.Node, _ string) (sitter.Node, error) {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "function_signature" {
		return node, nil
	}
	cnt, err := node.NamedChildCount(ctx)
	if err != nil || cnt == 0 {
		return node, nil
	}
	// Scan named children to find the identifier immediately before
	// formal_parameter_list. The structure is:
	//   [return_type][identifier:name][formal_parameter_list][...]
	var lastIdentifier sitter.Node
	foundIdent := false
	for i := uint64(0); i < cnt; i++ {
		ch, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		ck, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		if ck == "formal_parameter_list" {
			// The identifier we saw just before this is the function name.
			if foundIdent {
				return lastIdentifier, nil
			}
			break
		}
		if ck == "identifier" {
			lastIdentifier = ch
			foundIdent = true
		}
	}
	return node, nil
}

// dartIsExportedByName reports whether a Dart symbol is exported.
//
// Dart's visibility model: a symbol whose name begins with an underscore (_)
// is library-private (not exported). All other names are public (exported).
func dartIsExportedByName(name string) bool {
	return !strings.HasPrefix(name, "_")
}

// dartExtractImport extracts the import path from an import_or_export node.
//
// Dart grammar structure:
//
//	import_or_export
//	  library_import
//	    import_specification
//	      configurable_uri
//	        uri
//	          string_literal   ← text contains the quoted path
//
// We descend the chain and extract the string literal text, stripping quotes.
func dartExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	// Walk: import_or_export → library_import → import_specification →
	// configurable_uri → uri → string_literal
	cur := node
	for _, targetKind := range []string{
		"library_import",
		"import_specification",
		"configurable_uri",
		"uri",
	} {
		next, ok := firstNamedChildOfKind(ctx, cur, targetKind)
		if !ok {
			return "", ""
		}
		cur = next
	}

	// cur is now the uri node; its first child should be a string_literal.
	strLit, ok := firstNamedChildOfKind(ctx, cur, "string_literal")
	if !ok {
		// Fallback: use uri node text directly.
		strLit = cur
	}

	sb, _ := strLit.StartByte(ctx)
	eb, _ := strLit.EndByte(ctx)
	if int(eb) > len(source) {
		return "", ""
	}
	raw := strings.TrimSpace(source[sb:eb])
	// Strip surrounding quotes (' or ").
	raw = strings.Trim(raw, `'"`)
	if raw == "" {
		return "", ""
	}
	path = raw
	// Name = last path segment (e.g. "dart:async" → "async",
	// "package:flutter/material.dart" → "material.dart").
	segments := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == ':' })
	if len(segments) > 0 {
		name = segments[len(segments)-1]
	} else {
		name = path
	}
	return name, path
}

// firstNamedChildOfKind returns the first named child of node with the given kind.
// The bool return is true when a matching child was found.
// Used by Dart's import extractor and Pascal's import extractor.
func firstNamedChildOfKind(ctx context.Context, node sitter.Node, targetKind string) (sitter.Node, bool) {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return sitter.Node{}, false
	}
	for i := uint64(0); i < cnt; i++ {
		ch, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		k, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		if k == targetKind {
			return ch, true
		}
	}
	return sitter.Node{}, false
}

// Ensure dartIsExportedByName satisfies the IsExportedByName signature.
var _ func(string) bool = dartIsExportedByName

// Ensure dartExtractImport satisfies the ExtractImport signature.
var _ func(context.Context, sitter.Node, string) (string, string) = dartExtractImport

// Ensure dartResolveBody satisfies the ResolveBody signature.
var _ func(context.Context, sitter.Node, string) (sitter.Node, error) = dartResolveBody
