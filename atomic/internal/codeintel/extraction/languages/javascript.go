package languages

// JavaScript language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-lang-details; see main.go
// and probe2.go for exact output):
//
//   Top-level (direct children of program):
//     import_statement       — import { X } from 'y';
//     export_statement       — wraps the actual declaration
//     lexical_declaration    — const x = ...; let x = ...;
//     variable_declaration   — var x = ...;
//     expression_statement   — bare calls, require() calls
//
//   Inside lexical_declaration / variable_declaration:
//     variable_declarator    — holds the name (field "name") and value
//
//   Inside export_statement (via "declaration" field):
//     class_declaration      — export class Widget { ... }
//     function_declaration   — export function make() { ... }
//     lexical_declaration    — export const helper = (x) => x * 2;
//
//   "export default function foo()" emits export_statement with a
//   function_declaration child that starts at the "function" keyword, so
//   text-prefix lookback only sees "default " (not "export ").
//
//   Named iterator also sees:
//     method_definition      — class method implementations
//     call_expression        — function/method calls
//     arrow_function         — (x) => x + 1 (inside lexical_declaration)
//
// IsExported strategy: ExportStatementTypes = {"export_statement"}.
// The engine detects the export_statement wrapper and sets forceExported=true
// for all semantic children. This handles export, export default, and all JS
// export forms correctly. See typescript.go for full design rationale.
//
// Note: JavaScript has no interfaces, no type aliases, no enums (in the grammar
// sense). Only ClassTypes and FunctionTypes are wired.
//
// Arrow functions as exports: "export const helper = (x) => x * 2" — the
// lexical_declaration is the child of export_statement. The arrow_function is
// the value inside the variable_declarator. The extractor descends into
// lexical_declaration (unmatched) → variable_declarator (unmatched) → sees the
// arrow_function as a child. Arrow functions are NOT added to FunctionTypes here
// because nameFromNode can't reliably extract a name from them (the name lives in
// the parent variable_declarator). Deferred to CP8 (lexical_declaration → arrow
// function with name from declarator).

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// JavaScriptExtractor returns the LanguageExtractor config for JavaScript source
// files (.js, .mjs, .cjs). JSX (.jsx) uses a different grammar.
//
// Node-type strings are verified by parsing real JavaScript via the wazero
// binding (see tmp/probe-lang-details/main.go and probe2.go).
func JavaScriptExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// function_declaration covers named functions.
		// method_definition covers class methods.
		FunctionTypes: extraction.TypeSet("function_declaration"),
		MethodTypes:   extraction.TypeSet("method_definition"),

		// class_declaration covers classes.
		ClassTypes: extraction.TypeSet("class_declaration"),

		// No InterfaceTypes, TypeAliasTypes, EnumTypes — JavaScript grammar lacks them.

		// lexical_declaration covers const/let declarations.
		// variable_declaration covers var declarations.
		// jsResolveVariableDeclarator unwraps to the first variable_declarator child
		// so nameFromNode finds the identifier via NameField="name".
		// Known limitation: multi-declarator statements (const a=1, b=2) only produce
		// a node for the first declarator.
		// Known simplification: arrow-function consts (const f = () => {}) are
		// extracted as NodeKindVariable, not NodeKindFunction.
		VariableTypes: extraction.TypeSet("lexical_declaration", "variable_declaration"),

		// import_statement covers ESM imports.
		ImportTypes: extraction.TypeSet("import_statement"),

		// call_expression covers function and method calls.
		CallTypes: extraction.TypeSet("call_expression"),

		// export_statement is the AST-level export wrapper. The engine sets
		// forceExported=true for all semantic children when it encounters this type.
		ExportStatementTypes: extraction.TypeSet("export_statement"),

		// JSXElementTypes: populated here so .js files with embedded JSX (e.g.
		// with the appropriate babel/esbuild pragma) emit component refs if the JS
		// grammar happens to parse jsx_element nodes. The JS grammar does not
		// reliably emit JSX nodes without mode flags — .jsx files use LangTSX via
		// JSXExtractor() instead. This is a safe no-op for standard .js files.
		JSXElementTypes: extraction.TypeSet("jsx_element", "jsx_self_closing_element"),

		// FieldAssignmentTypes enables EE3 field-assignment capture.
		// Same node type as TypeScript — "assignment_expression" covers `x = y`.
		// Enables the callback synthesizer to find `this.onData = handler` sites.
		FieldAssignmentTypes: extraction.TypeSet("assignment_expression"),

		// Field names in the JavaScript grammar (same as TypeScript for shared kinds).
		NameField:   "name",
		BodyField:   "body",
		ParamsField: "parameters",

		// ResolveBody: unwrap lexical_declaration / variable_declaration to its first
		// variable_declarator child so nameFromNode finds the identifier via NameField.
		ResolveBody: jsResolveVariableDeclarator,

		// GetSignature for functions and methods.
		GetSignature: jsGetSignature,

		// ExtractImport handles import_statement (ESM) nodes.
		// Note: require() / CommonJS imports are not handled this checkpoint.
		ExtractImport: jsExtractImport,
	}
}

// jsResolveVariableDeclarator unwraps a lexical_declaration or variable_declaration
// to its first variable_declarator child so nameFromNode resolves the identifier
// via the "name" field. All other node types are returned unchanged.
func jsResolveVariableDeclarator(ctx context.Context, node sitter.Node, source string) (sitter.Node, error) {
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

// jsGetSignature returns the signature text for functions and methods.
func jsGetSignature(ctx context.Context, node sitter.Node, source string) string {
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

// jsExtractImport extracts the module path from an import_statement node.
// Supports: import { X } from 'path'; import X from 'path'; import 'path';
func jsExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
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

	// Bare import 'path'; (side-effect import)
	rest := strings.TrimPrefix(text, "import ")
	rest = strings.TrimSuffix(rest, ";")
	rest = strings.Trim(rest, `"'`)
	if rest != "" {
		parts := strings.Split(rest, "/")
		return parts[len(parts)-1], rest
	}

	return "", ""
}
