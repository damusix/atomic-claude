// Package languages provides per-grammar LanguageExtractor configurations.
// Each config maps the grammar's node-type strings to semantic roles and
// supplies language-specific hook implementations.
//
// Node-type strings are VERIFIED against the real grammar by parsing a sample
// file and inspecting the emitted kinds (see tmp/verify_go_grammar.go). Do not
// guess — wrong strings mean silently missing symbols.
package languages

import (
	"context"
	"strings"
	"unicode"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// GoExtractor returns the LanguageExtractor config for Go source files.
//
// Verified node-type strings (parsed from a real Go source via the wazero
// binding; see tmp/verify_go_grammar.go and tmp/probe_typespec/main.go):
//
//   - function_declaration  — top-level functions
//   - method_declaration    — methods (with receiver)
//   - type_declaration      — wrapper: may contain type_spec or type_alias
//   - type_spec             — concrete type body: struct_type / interface_type /
//     plain type-identifier (named type, e.g. "type Status int")
//   - struct_type           — struct body (children are field_declaration_list)
//   - interface_type        — interface body
//   - type_alias            — type Foo = Bar  (distinct node from type_spec)
//   - field_declaration     — struct field
//   - import_declaration    — import block or single import
//   - call_expression       — function/method call
//   - const_declaration     — iota / enum-like constant block
//
// Note: short_var_declaration (:= assignments) is NOT wired — deferred to a
// future checkpoint.
//
// Go grammar fields:
//   - function_declaration: name (identifier), parameters, result, body
//   - method_declaration:   receiver (parameter_list), name (field_identifier),
//     parameters, result, body
//   - type_spec:            name (type_identifier), type (struct_type /
//     interface_type / plain type-identifier)
//   - type_alias:           name (type_identifier)
//
// Type-kind disambiguation: Go uses one grammar node (type_declaration) for
// structs, interfaces, aliases, and named types. The ResolveKind hook inspects
// the first named child of type_declaration to distinguish them:
//   - type_declaration > type_spec > struct_type  → NodeKindStruct
//   - type_declaration > type_spec > interface_type → NodeKindInterface
//   - type_declaration > type_alias                → NodeKindTypeAlias
//   - type_declaration > type_spec > (other)       → NodeKindTypeAlias
//     (e.g. "type Status int" — no dedicated NodeKind in appendix C)
func GoExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// Verified node-type strings.
		FunctionTypes: extraction.TypeSet("function_declaration"),
		MethodTypes:   extraction.TypeSet("method_declaration"),
		// type_declaration covers struct, interface, type alias, and named types.
		// ResolveKind distinguishes them at runtime by inspecting the inner child.
		StructTypes:    extraction.TypeSet("type_declaration"),
		InterfaceTypes: nil, // handled via StructTypes + ResolveKind
		EnumTypes:      extraction.TypeSet("const_declaration"),
		TypeAliasTypes: nil, // handled via StructTypes + ResolveKind
		FieldTypes:     extraction.TypeSet("field_declaration"),
		// import_spec rather than import_declaration: the walker visits
		// import_declaration (no match → recurse), then import_spec_list (no
		// match → recurse), and finally each import_spec individually — one
		// extractImport call per path. This is the fix for F-61: a grouped
		// import block with N paths now emits N UnresolvedReferences instead
		// of just 1.
		ImportTypes: extraction.TypeSet("import_spec"),
		CallTypes:   extraction.TypeSet("call_expression"),

		// Field names in the Go grammar.
		NameField:   "name",
		BodyField:   "body",
		ParamsField: "parameters",
		ReturnField: "result",

		// ResolveBody unwraps type_declaration → type_spec (or type_alias).
		ResolveBody: goResolveBody,

		// ResolveKind inspects the inner child of type_declaration to return the
		// correct semantic NodeKind (struct / interface / type_alias).
		ResolveKind: goResolveKind,

		// GetSignature builds a human-readable signature for functions/methods.
		GetSignature: goGetSignature,

		// IsExportedByName: in Go, exported = first rune is uppercase.
		IsExportedByName: goIsExportedByName,

		// ExtractImport extracts the import path from an import_declaration or
		// import_spec node.
		ExtractImport: goExtractImport,
	}
}

// goResolveBody unwraps a type_declaration node to its first named child,
// which is either a type_spec (struct / interface / named type) or a
// type_alias node (type Foo = Bar).  Returns the same node unchanged for any
// other input.
func goResolveBody(ctx context.Context, node sitter.Node, source string) (sitter.Node, error) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return node, nil
	}
	if kind != "type_declaration" {
		return node, nil
	}
	// type_declaration always has exactly one named child: type_spec or type_alias.
	cnt, err := node.NamedChildCount(ctx)
	if err != nil || cnt == 0 {
		return node, nil
	}
	child, err := node.NamedChild(ctx, 0)
	if err != nil {
		return node, nil
	}
	return child, nil
}

// goResolveKind inspects a type_declaration node and returns the correct
// semantic NodeKind by examining the structure of its children:
//
//	type_declaration > type_spec > struct_type    → NodeKindStruct
//	type_declaration > type_spec > interface_type → NodeKindInterface
//	type_declaration > type_alias                 → NodeKindTypeAlias
//	type_declaration > type_spec > (other)        → NodeKindTypeAlias
//	  (named type, e.g. "type Status int" — no dedicated NodeKind in appendix C)
//
// Node strings verified by real parse: see tmp/probe_typespec/main.go.
func goResolveKind(ctx context.Context, node sitter.Node, source string) types.NodeKind {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "type_declaration" {
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

	switch firstKind {
	case "type_alias":
		// type Foo = Bar — the grammar emits type_alias (not type_spec).
		return types.NodeKindTypeAlias

	case "type_spec":
		// type_spec wraps the actual type body. Its second named child (after
		// the type_identifier name) tells us the concrete kind.
		specCnt, err := firstChild.NamedChildCount(ctx)
		if err != nil || specCnt < 2 {
			// Only the name child present — treat as named type (type_alias).
			return types.NodeKindTypeAlias
		}
		// The second named child of type_spec is the type body.
		typeBody, err := firstChild.NamedChild(ctx, 1)
		if err != nil {
			return types.NodeKindTypeAlias
		}
		bodyKind, err := typeBody.Kind(ctx)
		if err != nil {
			return types.NodeKindTypeAlias
		}
		switch bodyKind {
		case "struct_type":
			return types.NodeKindStruct
		case "interface_type":
			return types.NodeKindInterface
		default:
			// Named type over another type (e.g. "type Status int").
			// No dedicated NodeKind in appendix C; use NodeKindTypeAlias.
			return types.NodeKindTypeAlias
		}

	default:
		return types.NodeKindStruct
	}
}

// goGetSignature returns the text of the function/method signature (everything
// before the body), which is useful for search and display.
func goGetSignature(ctx context.Context, node sitter.Node, source string) string {
	kind, err := node.Kind(ctx)
	if err != nil {
		return ""
	}
	if kind != "function_declaration" && kind != "method_declaration" {
		return ""
	}
	sb, err := node.StartByte(ctx)
	if err != nil {
		return ""
	}

	// Find the body node to know where the signature ends.
	bodyNode, err := node.ChildByFieldName(ctx, "body")
	if err != nil {
		return ""
	}
	isNull, _ := bodyNode.IsNull(ctx)
	if isNull {
		// No body (function type, not declaration) — use full text.
		eb, _ := node.EndByte(ctx)
		t := source[sb:eb]
		t = strings.TrimSpace(t)
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

// goIsExportedByName reports whether a Go symbol name is exported.
// In Go, a symbol is exported if and only if its first rune is an uppercase
// Unicode letter (spec: "An identifier may be exported to permit access to it
// from another package. An identifier is exported if both: the first character
// of the identifier's name is a Unicode upper case letter").
func goIsExportedByName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

// goExtractImport extracts the import path from an import_declaration or
// import_spec node. Returns ("", "") when the path cannot be extracted.
func goExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return "", ""
	}

	if kind == "import_declaration" {
		// Walk children: import_spec_list > import_spec, or single import_spec.
		cnt, _ := node.NamedChildCount(ctx)
		for i := uint64(0); i < cnt; i++ {
			child, err := node.NamedChild(ctx, i)
			if err != nil {
				continue
			}
			ck, _ := child.Kind(ctx)
			if ck == "import_spec_list" {
				// Multi-import: extract first path as representative name.
				innerCnt, _ := child.NamedChildCount(ctx)
				for j := uint64(0); j < innerCnt; j++ {
					spec, err := child.NamedChild(ctx, j)
					if err != nil {
						continue
					}
					sk, _ := spec.Kind(ctx)
					if sk == "import_spec" {
						n, p := extractImportSpec(ctx, spec, source)
						if p != "" {
							return n, p
						}
					}
				}
			}
			if ck == "import_spec" {
				n, p := extractImportSpec(ctx, child, source)
				if p != "" {
					return n, p
				}
			}
		}
		return "", ""
	}

	if kind == "import_spec" {
		return extractImportSpec(ctx, node, source)
	}

	return "", ""
}

// extractImportSpec extracts name and path from a single import_spec node.
// import_spec = [ alias_identifier ] string_literal
func extractImportSpec(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	cnt, _ := node.NamedChildCount(ctx)
	for i := uint64(0); i < cnt; i++ {
		child, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		ck, _ := child.Kind(ctx)
		if ck == "interpreted_string_literal" {
			sb, _ := child.StartByte(ctx)
			eb, _ := child.EndByte(ctx)
			raw := source[sb:eb]
			// Strip surrounding quotes.
			raw = strings.Trim(raw, `"`)
			path = raw
			// Name = last path segment.
			parts := strings.Split(raw, "/")
			name = parts[len(parts)-1]
			return name, path
		}
	}
	return "", ""
}
