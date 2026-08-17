package languages

// Node-type strings are read from the live JavaScript grammar — do not guess.
// It is the same grammar family as TypeScript, so typescript.go's header notes
// on generator nodes, structural export detection, and function-scope
// suppression apply here unchanged.
//
// An arrow function stays out of FunctionTypes: its name lives on the parent
// variable_declarator, out of reach of the name-from-node path.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// JavaScriptExtractor returns the LanguageExtractor config for JavaScript source
// files (.js, .mjs, .cjs). JSX (.jsx) uses a different grammar; see JSXExtractor.
func JavaScriptExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		FunctionTypes: extraction.TypeSet("function_declaration"),
		MethodTypes:   extraction.TypeSet("method_definition"),

		ClassTypes: extraction.TypeSet("class_declaration"),

		// The grammar has no interfaces, type aliases, or enums to wire.

		// Two known simplifications: "const a = 1, b = 2" yields a node only for
		// the first declarator, and "const f = () => {}" is a variable, not a
		// function.
		VariableTypes: extraction.TypeSet("lexical_declaration", "variable_declaration"),

		FunctionScopeTypes: extraction.TypeSet("arrow_function", "function_expression", "generator_function"),

		ImportTypes: extraction.TypeSet("import_statement"),

		CallTypes: extraction.TypeSet("call_expression"),

		ExportStatementTypes: extraction.TypeSet("export_statement"),

		// A no-op for ordinary .js, whose grammar needs mode flags before it
		// emits JSX at all; .jsx files go through JSXExtractor and the tsx
		// grammar instead.
		JSXElementTypes: extraction.TypeSet("jsx_element", "jsx_self_closing_element"),

		// Field assignments, which is how the callback synthesizer finds a site
		// like "this.onData = handler".
		FieldAssignmentTypes: extraction.TypeSet("assignment_expression"),

		NameField:   "name",
		BodyField:   "body",
		ParamsField: "parameters",

		ResolveBody: jsResolveVariableDeclarator,

		GetSignature: jsGetSignature,

		// ESM only; require() goes unextracted.
		ExtractImport: jsExtractImport,
	}
}

// jsResolveVariableDeclarator unwraps a declaration to its first
// variable_declarator, the node that actually carries the "name" field. Any
// other node passes through unchanged.
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

// jsGetSignature returns everything before the body, truncated to 200 bytes.
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

// jsExtractImport returns the module path of an import_statement, side-effect
// form included, and the name importNodeName derives from it.
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

	if idx := strings.Index(text, " from "); idx >= 0 {
		rest := strings.TrimSpace(text[idx+6:])
		rest = strings.TrimSuffix(rest, ";")
		rest = strings.Trim(rest, `"'`)
		if rest != "" {
			return importNodeName(rest), rest
		}
	}

	// Side-effect form: import 'path';
	rest := strings.TrimPrefix(text, "import ")
	rest = strings.TrimSuffix(rest, ";")
	rest = strings.Trim(rest, `"'`)
	if rest != "" {
		return importNodeName(rest), rest
	}

	return "", ""
}
