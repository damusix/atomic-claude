// Package languages provides per-grammar LanguageExtractor configurations.
// Each config maps the grammar's node-type strings to semantic roles and
// supplies language-specific hook implementations.
//
// Every node-type string here was read from the real grammar by parsing a sample
// file and inspecting the emitted kinds. Do not guess one: a wrong string does
// not fail, it silently drops symbols.
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
func GoExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		FunctionTypes: extraction.TypeSet("function_declaration"),
		MethodTypes:   extraction.TypeSet("method_declaration"),
		// One node type covers struct, interface, alias, and named type, so
		// ResolveKind has to separate them at runtime. StructTypes is the arm
		// that runs it, which is why the other three are left nil.
		StructTypes:    extraction.TypeSet("type_declaration"),
		InterfaceTypes: nil,
		EnumTypes:      extraction.TypeSet("const_declaration"),
		TypeAliasTypes: nil,
		FieldTypes:     extraction.TypeSet("field_declaration"),
		// import_spec, not import_declaration: the walker recurses past the
		// declaration and its spec list to reach each spec, so a grouped import
		// block of N paths emits N references rather than one.
		ImportTypes: extraction.TypeSet("import_spec"),
		CallTypes:   extraction.TypeSet("call_expression"),

		NameField:   "name",
		BodyField:   "body",
		ParamsField: "parameters",
		ReturnField: "result",

		ResolveBody: goResolveBody,

		ResolveKind: goResolveKind,

		GetSignature: goGetSignature,

		IsExportedByName: goIsExportedByName,

		ExtractImport: goExtractImport,
	}
}

// goResolveBody unwraps a type_declaration to its single named child, a
// type_spec or a type_alias. Any other node passes through unchanged.
func goResolveBody(ctx context.Context, node sitter.Node, source string) (sitter.Node, error) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return node, nil
	}
	if kind != "type_declaration" {
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
	return child, nil
}

// goResolveKind walks a type_declaration down to the node that names the kind:
// type_alias, or the body under a type_spec. A named type such as "type Status
// int" reports as a type alias, there being no node kind of its own for it.
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
		return types.NodeKindTypeAlias

	case "type_spec":
		// First named child is the name; the second is the type body.
		specCnt, err := firstChild.NamedChildCount(ctx)
		if err != nil || specCnt < 2 {
			return types.NodeKindTypeAlias
		}
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
			return types.NodeKindTypeAlias
		}

	default:
		return types.NodeKindStruct
	}
}

// goGetSignature returns everything before the body, truncated to 200 bytes.
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

	bodyNode, err := node.ChildByFieldName(ctx, "body")
	if err != nil {
		return ""
	}
	isNull, _ := bodyNode.IsNull(ctx)
	if isNull {
		// A function type rather than a declaration: it is all signature.
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

// goIsExportedByName applies Go's rule: exported iff the first rune is an
// uppercase Unicode letter.
func goIsExportedByName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

// goExtractImport handles both an import_spec and a whole import_declaration,
// returning ("", "") when no path can be extracted. Given a declaration it
// reports only the first path it finds; the config registers import_spec so that
// each path in a group is visited separately.
func goExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return "", ""
	}

	if kind == "import_declaration" {
		cnt, _ := node.NamedChildCount(ctx)
		for i := uint64(0); i < cnt; i++ {
			child, err := node.NamedChild(ctx, i)
			if err != nil {
				continue
			}
			ck, _ := child.Kind(ctx)
			if ck == "import_spec_list" {
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

// extractImportSpec returns an import_spec's path and its last segment as the
// name. An alias, if the spec has one, is ignored.
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
			raw = strings.Trim(raw, `"`)
			path = raw
			parts := strings.Split(raw, "/")
			name = parts[len(parts)-1]
			return name, path
		}
	}
	return "", ""
}
