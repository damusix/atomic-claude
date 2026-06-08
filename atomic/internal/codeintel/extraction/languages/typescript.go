package languages

// TypeScript language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-lang-details; see probe2.go
// for exact output):
//
//   Top-level (direct children of program):
//     import_statement       — import { X } from "y"; import X from "y";
//     export_statement       — wraps the actual declaration via "declaration" field
//     lexical_declaration    — const x = ...; let x = ...; (non-exported, top-level)
//     variable_declaration   — var x = ...;
//
//   Inside export_statement (via "declaration" field):
//     interface_declaration  — export interface Widget { ... }
//     type_alias_declaration — export type Handler = ...
//     enum_declaration       — export enum Color { Red, Green }
//     class_declaration      — export class Button { ... }
//     function_declaration   — export function makeButton() { ... }
//     lexical_declaration    — export const helper = (x) => x * 2;
//
//   Inside lexical_declaration / variable_declaration:
//     variable_declarator    — holds the name (field "name") and value
//
//   "export default function foo()" emits export_statement with a
//   function_declaration child. The function_declaration starts at the "function"
//   keyword — text lookback would only see "default " (not "export "), so the
//   old 8-byte text-scan missed this case.
//
//   Named iterator also sees (inside class/function bodies):
//     method_definition      — method implementations
//     method_signature       — method signatures (in interface bodies)
//     call_expression        — function/method call sites
//
// IsExported strategy: ExportStatementTypes = {"export_statement"}.
// The engine detects when it is visiting children of an export_statement and
// sets forceExported=true for all semantic children it extracts. This is the
// AST-based approach — it handles export, export default, and all export forms
// without text scanning. The binding has no Parent() method, so detecting the
// parent via ExportStatementTypes at the grandparent-visits-child level is the
// correct idiomatic approach.
//
// Fields:
//   - interface_declaration, class_declaration: name field = "name"
//   - function_declaration: name field = "name"
//   - type_alias_declaration: name field = "name"
//   - enum_declaration: name field = "name"
//   - method_definition: name field = "name"

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// TypeScriptExtractor returns the LanguageExtractor config for TypeScript source
// files (.ts). TSX uses a separate grammar and is not handled by this config.
//
// Node-type strings are verified by parsing real TypeScript via the wazero
// binding (see tmp/probe-lang-details/main.go and probe2.go).
func TypeScriptExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// function_declaration covers named functions.
		// method_definition covers class methods; method_signature covers interface methods.
		FunctionTypes: extraction.TypeSet("function_declaration"),
		MethodTypes:   extraction.TypeSet("method_definition", "method_signature"),

		// class_declaration covers classes.
		ClassTypes: extraction.TypeSet("class_declaration"),

		// interface_declaration covers TypeScript interfaces.
		InterfaceTypes: extraction.TypeSet("interface_declaration"),

		// type_alias_declaration covers "type X = ..." type aliases.
		TypeAliasTypes: extraction.TypeSet("type_alias_declaration"),

		// enum_declaration covers TypeScript enums.
		EnumTypes: extraction.TypeSet("enum_declaration"),

		// lexical_declaration covers const/let declarations.
		// variable_declaration covers var declarations.
		// Both are matched here; tsResolveVariableDeclarator unwraps to the first
		// variable_declarator so nameFromNode finds the identifier via NameField="name".
		// Known limitation: multi-declarator statements (const a=1, b=2) only
		// produce a node for the first declarator.
		// Known simplification: arrow-function consts (const f = () => {}) are
		// extracted as NodeKindVariable, not NodeKindFunction.
		VariableTypes: extraction.TypeSet("lexical_declaration", "variable_declaration"),

		// import_statement covers all import forms.
		ImportTypes: extraction.TypeSet("import_statement"),

		// call_expression covers function and method calls.
		CallTypes: extraction.TypeSet("call_expression"),

		// export_statement is the AST-level export wrapper. When the engine sees
		// this node type, it sets forceExported=true for all semantic children it
		// extracts. This correctly handles export, export default, and all TS export
		// forms without relying on text-prefix scanning.
		ExportStatementTypes: extraction.TypeSet("export_statement"),

		// JSXElementTypes: populated here so mixed .ts files that contain JSX
		// (rare, but possible with certain tsconfig settings) emit component refs.
		// The tsx grammar is not used for .ts files — the TS grammar does not emit
		// these node types, so this is a safe no-op for standard .ts files.
		JSXElementTypes: extraction.TypeSet("jsx_element", "jsx_self_closing_element"),

		// FieldAssignmentTypes enables EE3 field-assignment capture.
		// "assignment_expression" is the TS/JS grammar node for `x = y` expressions.
		// The extractor walks it for member_expression LHS + callable RHS and emits
		// a "references" UnresolvedReference with Arguments[0] = "field:<fieldName>"
		// as the discriminator sentinel for the callback synthesizer.
		// Verified via tmp/ probe (assignment_expression node confirmed in TS grammar).
		FieldAssignmentTypes: extraction.TypeSet("assignment_expression"),

		// Field names in the TypeScript grammar.
		NameField:   "name",
		BodyField:   "body",
		ParamsField: "parameters",
		ReturnField: "return_type",

		// ResolveBody: unwrap lexical_declaration / variable_declaration to its first
		// variable_declarator child so nameFromNode finds the identifier via NameField.
		ResolveBody: tsResolveVariableDeclarator,

		// GetSignature extracts the signature text for functions and methods.
		GetSignature: tsGetSignature,

		// ExtractImport extracts the import path from import_statement nodes.
		ExtractImport: tsExtractImport,

		// ExtractHeritage extracts extends/implements base types from class and
		// interface declarations.
		ExtractHeritage: tsExtractHeritage,
	}
}

// tsResolveVariableDeclarator unwraps a lexical_declaration or variable_declaration
// to its first variable_declarator child. This lets nameFromNode resolve the
// declarator's "name" field (an identifier) rather than looking for a "name" field
// on the declaration itself (which does not exist in the TS grammar).
//
// For all other node types it returns the node unchanged, so it is safe to use as
// the blanket ResolveBody hook for the TS config.
func tsResolveVariableDeclarator(ctx context.Context, node sitter.Node, source string) (sitter.Node, error) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return node, nil
	}
	if kind != "lexical_declaration" && kind != "variable_declaration" {
		return node, nil
	}
	cnt, err := node.NamedChildCount(ctx)
	if err != nil || cnt == 0 {
		return node, nil
	}
	child, err := node.NamedChild(ctx, 0)
	if err != nil {
		return node, nil
	}
	ck, err := child.Kind(ctx)
	if err != nil || ck != "variable_declarator" {
		return node, nil
	}
	return child, nil
}

// tsGetSignature returns the text of a function or method signature (everything
// before the body block).
func tsGetSignature(ctx context.Context, node sitter.Node, source string) string {
	kind, err := node.Kind(ctx)
	if err != nil {
		return ""
	}
	if kind != "function_declaration" && kind != "method_definition" {
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

// tsExtractImport extracts the module path from a TypeScript import_statement.
// Supports: import { X } from "path"; import X from "path"; import "path";
func tsExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "import_statement" {
		return "", ""
	}
	sb, _ := node.StartByte(ctx)
	eb, _ := node.EndByte(ctx)
	if int(eb) > len(source) {
		return "", ""
	}
	text := source[sb:eb]

	// Extract quoted path from "from 'path'" or "from \"path\"".
	if idx := strings.Index(text, " from "); idx >= 0 {
		rest := strings.TrimSpace(text[idx+6:])
		rest = strings.TrimSuffix(rest, ";")
		rest = strings.Trim(rest, `"'`)
		if rest != "" {
			parts := strings.Split(rest, "/")
			return parts[len(parts)-1], rest
		}
	}

	// Bare import "path"; (side-effect import)
	rest := strings.TrimPrefix(text, "import ")
	rest = strings.TrimSuffix(rest, ";")
	rest = strings.Trim(rest, `"'`)
	if rest != "" {
		parts := strings.Split(rest, "/")
		return parts[len(parts)-1], rest
	}

	return "", ""
}

// tsExtractHeritage extracts base-type references from TypeScript class and
// interface declarations.
//
// Grammar layout (verified by real parse probe):
//
//	class_declaration
//	  class_heritage
//	    extends_clause       — "extends Animal"
//	      type_identifier    — "Animal"  (or identifier)
//	    implements_clause    — "implements Speaker, Runner"
//	      type_identifier    — "Speaker"
//	      type_identifier    — "Runner"
//
//	interface_declaration
//	  extends_type_clause    — "extends Printable"
//	    type_identifier      — "Printable"
//
// All extends_clause children → EdgeKindExtends.
// All implements_clause children → EdgeKindImplements.
// All extends_type_clause children (on interface) → EdgeKindExtends.
func tsExtractHeritage(ctx context.Context, node sitter.Node, source string) []extraction.HeritageRef {
	kind, err := node.Kind(ctx)
	if err != nil {
		return nil
	}

	var refs []extraction.HeritageRef

	switch kind {
	case "class_declaration":
		refs = tsWalkClassHeritage(ctx, node, source)
	case "interface_declaration":
		refs = tsWalkInterfaceHeritage(ctx, node, source)
	}

	return refs
}

// tsWalkClassHeritage walks a class_declaration for class_heritage children.
func tsWalkClassHeritage(ctx context.Context, node sitter.Node, source string) []extraction.HeritageRef {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return nil
	}
	var refs []extraction.HeritageRef
	for i := uint64(0); i < cnt; i++ {
		ch, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		ck, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		if ck != "class_heritage" {
			continue
		}
		// Walk children of class_heritage: extends_clause + implements_clause.
		hcnt, err := ch.NamedChildCount(ctx)
		if err != nil {
			continue
		}
		for j := uint64(0); j < hcnt; j++ {
			hch, err := ch.NamedChild(ctx, j)
			if err != nil {
				continue
			}
			hck, err := hch.Kind(ctx)
			if err != nil {
				continue
			}
			switch hck {
			case "extends_clause":
				refs = append(refs, tsCollectTypeRefs(ctx, hch, source, types.EdgeKindExtends)...)
			case "implements_clause":
				refs = append(refs, tsCollectTypeRefs(ctx, hch, source, types.EdgeKindImplements)...)
			}
		}
	}
	return refs
}

// tsWalkInterfaceHeritage walks an interface_declaration for extends_type_clause.
func tsWalkInterfaceHeritage(ctx context.Context, node sitter.Node, source string) []extraction.HeritageRef {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return nil
	}
	var refs []extraction.HeritageRef
	for i := uint64(0); i < cnt; i++ {
		ch, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		ck, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		if ck != "extends_type_clause" {
			continue
		}
		refs = append(refs, tsCollectTypeRefs(ctx, ch, source, types.EdgeKindExtends)...)
	}
	return refs
}

// tsCollectTypeRefs collects type_identifier / identifier children of a clause
// node, returning one HeritageRef per child.
func tsCollectTypeRefs(ctx context.Context, clause sitter.Node, source string, edgeKind types.EdgeKind) []extraction.HeritageRef {
	cnt, err := clause.NamedChildCount(ctx)
	if err != nil {
		return nil
	}
	var refs []extraction.HeritageRef
	for i := uint64(0); i < cnt; i++ {
		ch, err := clause.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		ck, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		if ck != "type_identifier" && ck != "identifier" {
			continue
		}
		sb, _ := ch.StartByte(ctx)
		eb, _ := ch.EndByte(ctx)
		if int(eb) > len(source) {
			continue
		}
		name := strings.TrimSpace(source[sb:eb])
		if name != "" {
			refs = append(refs, extraction.HeritageRef{Name: name, Kind: edgeKind})
		}
	}
	return refs
}
