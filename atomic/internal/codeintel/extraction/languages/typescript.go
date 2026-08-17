package languages

// Node-type strings are read from the live TypeScript grammar — do not guess.
// A generator expression is a bare "generator_function", for one, not the
// "generator_function_expression" its siblings' naming suggests.
//
// Exports are detected structurally, through ExportStatementTypes: the engine
// force-marks the children of an export_statement as exported. Scanning the text
// before a declaration cannot work, because "export default function foo()"
// gives the function_declaration a start byte just past "default ".
//
// Two constructs look like they should be affected by FunctionScopeTypes
// suppression and are not. A for-of binding is a bare identifier under
// for_in_statement, so VariableTypes never matches it to begin with. A
// namespace body hangs off internal_module, which is not a function scope, so
// its consts survive.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// TypeScriptExtractor returns the LanguageExtractor config for TypeScript source
// files (.ts). TSX uses a separate grammar; see TSXExtractor.
func TypeScriptExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		FunctionTypes: extraction.TypeSet("function_declaration"),
		MethodTypes:   extraction.TypeSet("method_definition", "method_signature"),

		ClassTypes: extraction.TypeSet("class_declaration"),

		InterfaceTypes: extraction.TypeSet("interface_declaration"),

		TypeAliasTypes: extraction.TypeSet("type_alias_declaration"),

		EnumTypes: extraction.TypeSet("enum_declaration"),

		// Two known simplifications: "const a = 1, b = 2" yields a node only for
		// the first declarator, and "const f = () => {}" is a variable, not a
		// function.
		VariableTypes: extraction.TypeSet("lexical_declaration", "variable_declaration"),

		// Scope openers that are not themselves declarations, such as a callback
		// passed as an argument. A VariableTypes match found underneath one of
		// these mints no node.
		FunctionScopeTypes: extraction.TypeSet("arrow_function", "function_expression", "generator_function"),

		ImportTypes: extraction.TypeSet("import_statement"),

		CallTypes: extraction.TypeSet("call_expression"),

		ExportStatementTypes: extraction.TypeSet("export_statement"),

		// A no-op for ordinary .ts, whose grammar never emits these; it earns its
		// place on the tsconfig settings that do allow JSX in a .ts file.
		JSXElementTypes: extraction.TypeSet("jsx_element", "jsx_self_closing_element"),

		// Field assignments ("x.y = fn") become references carrying a
		// "field:<name>" sentinel in Arguments[0], which the callback
		// synthesizer keys on.
		FieldAssignmentTypes: extraction.TypeSet("assignment_expression"),

		NameField:   "name",
		BodyField:   "body",
		ParamsField: "parameters",
		ReturnField: "return_type",

		ResolveBody: tsResolveVariableDeclarator,

		GetSignature: tsGetSignature,

		ExtractImport: tsExtractImport,

		ExtractHeritage: tsExtractHeritage,
	}
}

// tsResolveVariableDeclarator unwraps a declaration to its first
// variable_declarator, the node that actually carries the "name" field. Any
// other node passes through unchanged, so this is safe as the blanket
// ResolveBody hook.
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

// tsGetSignature returns everything before the body, truncated to 200 bytes.
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

// tsExtractImport returns the module path of an import_statement, side-effect
// form included, and the name importNodeName derives from it.
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

	if idx := strings.Index(text, " from "); idx >= 0 {
		rest := strings.TrimSpace(text[idx+6:])
		rest = strings.TrimSuffix(rest, ";")
		rest = strings.Trim(rest, `"'`)
		if rest != "" {
			return importNodeName(rest), rest
		}
	}

	// Side-effect form: import "path";
	rest := strings.TrimPrefix(text, "import ")
	rest = strings.TrimSuffix(rest, ";")
	rest = strings.Trim(rest, `"'`)
	if rest != "" {
		return importNodeName(rest), rest
	}

	return "", ""
}

// importNodeName names a package import by its full specifier, so that scoped
// and subpath packages keep their identity instead of colliding on a last
// segment, and a relative or absolute one by basename, since it resolves to a
// real file node anyway. Shared by the TypeScript and JavaScript extractors.
func importNodeName(specifier string) string {
	if strings.HasPrefix(specifier, ".") || strings.HasPrefix(specifier, "/") {
		parts := strings.Split(specifier, "/")
		return parts[len(parts)-1]
	}
	return specifier
}

// tsExtractHeritage collects base-type references. Classes and interfaces
// declare them under different node types, hence the two walkers.
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

// tsWalkClassHeritage reads the extends and implements clauses nested under a
// class_declaration's class_heritage child.
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
