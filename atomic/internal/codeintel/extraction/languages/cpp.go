package languages

// Node-type strings are read from the live C++ grammar — do not guess.
//
// As in C, function_definition carries no "name" field and the name has to be
// reached through the declarator.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// CppExtractor returns the LanguageExtractor config for C++ source files (.cpp, .cc, .cxx, .h, .hpp).
func CppExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		FunctionTypes: extraction.TypeSet("function_definition"),

		// StructTypes is the arm that runs ResolveKind, which all three of these
		// node types need.
		StructTypes: extraction.TypeSet("class_specifier", "struct_specifier", "enum_specifier"),

		ImportTypes: extraction.TypeSet("preproc_include"),

		CallTypes: extraction.TypeSet("call_expression"),

		// The three specifier types carry a "name" field; function_definition
		// does not, and falls through to the name-from-node path after
		// ResolveBody unwraps it.
		NameField: "name",

		ResolveBody: cppResolveBody,

		ResolveKind: cppResolveKind,

		IsExported: cppIsExported,

		// Same include syntax as C.
		ExtractImport: cExtractInclude,

		ExtractHeritage: cppExtractHeritage,
	}
}

// cppResolveBody unwraps function_definition to its function_declarator child,
// which is where the name-from-node fallback can reach the identifier. Any other
// node passes through unchanged.
func cppResolveBody(ctx context.Context, node sitter.Node, source string) (sitter.Node, error) {
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

// cppResolveKind maps each aggregate-type specifier to its own node kind.
func cppResolveKind(ctx context.Context, node sitter.Node, _ string) types.NodeKind {
	kind, err := node.Kind(ctx)
	if err != nil {
		return types.NodeKindStruct
	}
	switch kind {
	case "class_specifier":
		return types.NodeKindClass
	case "enum_specifier":
		return types.NodeKindEnum
	default:
		return types.NodeKindStruct
	}
}

// cppIsExported always reports true: C++ enforces access specifiers at the usage
// site, not the declaration, so no declaration-site bit corresponds to exported.
func cppIsExported(_ context.Context, _ sitter.Node, _ string) bool {
	return true
}

// cppExtractHeritage reads every type_identifier under a base_class_clause as an
// extends edge. C++ draws no extends/implements distinction, so a later
// promotion pass upgrades the edge once the target resolves to an interface or
// abstract class.
func cppExtractHeritage(ctx context.Context, node sitter.Node, source string) []extraction.HeritageRef {
	kind, err := node.Kind(ctx)
	if err != nil {
		return nil
	}
	if kind != "class_specifier" && kind != "struct_specifier" {
		return nil
	}

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
		if ck != "base_class_clause" {
			continue
		}
		bcnt, err := ch.NamedChildCount(ctx)
		if err != nil {
			continue
		}
		for j := uint64(0); j < bcnt; j++ {
			bch, err := ch.NamedChild(ctx, j)
			if err != nil {
				continue
			}
			bck, err := bch.Kind(ctx)
			if err != nil {
				continue
			}
			if bck == "access_specifier" {
				continue
			}
			if bck != "type_identifier" {
				continue
			}
			sb, _ := bch.StartByte(ctx)
			eb, _ := bch.EndByte(ctx)
			if int(eb) > len(source) {
				continue
			}
			name := strings.TrimSpace(source[sb:eb])
			if name != "" {
				refs = append(refs, extraction.HeritageRef{Name: name, Kind: types.EdgeKindExtends})
			}
		}
	}
	return refs
}
