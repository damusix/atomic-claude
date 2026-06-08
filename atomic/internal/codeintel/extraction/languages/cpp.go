package languages

// C++ language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-cp8a/ — C++ grammar):
//
//	Top-level (direct children of translation_unit):
//	  preproc_include      — #include <iostream>
//	  class_specifier      — class Shape { ... } / class Circle : public Shape { ... }
//	  struct_specifier     — struct Point { ... }
//	  enum_specifier       — enum class Color { ... }
//	  namespace_definition — namespace geometry { ... }
//	  function_definition  — double dist(double x) { ... } (top-level)
//
//	Named-iterator also sees (inside class bodies):
//	  function_definition  — double area() const { ... } (inside class)
//	  call_expression      — c.area() / Circle::unit() / geometry::distance(...)
//
// Name extraction for function_definition:
//
//	Like C, function_definition has no 'name' field. Name path:
//	  function_definition → declarator (function_declarator) → first-named-child (identifier)
//	ResolveBody unwraps function_definition → function_declarator.
//	NameField = "" so nameFromNode fallback fires; finds 'identifier'.
//	For methods inside classes, the declarator may be function_declarator with
//	an identifier child (the method name).
//
// Kind disambiguation (ResolveKind):
//
//	class_specifier  → NodeKindClass  (default for StructTypes)
//	struct_specifier → NodeKindStruct
//	enum_specifier   → NodeKindEnum
//
// IsExported rule: C++ has no export keyword for visibility. All symbols are
// treated as exported (IsExported = always true). C++'s access specifiers
// (public/private/protected) are enforced by the compiler at usage sites, not
// by a single 'exported' bit. Documenting this simplification here.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// CppExtractor returns the LanguageExtractor config for C++ source files (.cpp, .cc, .cxx, .h, .hpp).
//
// Node-type strings are verified by parsing real C++ via the wazero binding
// (see tmp/probe-cp8a/main.go, probe2.go, probe3.go, probe4.go).
func CppExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// function_definition covers all named functions and methods.
		FunctionTypes: extraction.TypeSet("function_definition"),

		// class_specifier, struct_specifier, and enum_specifier are all wired
		// through StructTypes so that ResolveKind can dispatch to the correct
		// semantic kind — same pattern as Go's type_declaration.
		StructTypes: extraction.TypeSet("class_specifier", "struct_specifier", "enum_specifier"),

		// preproc_include covers all #include directives.
		ImportTypes: extraction.TypeSet("preproc_include"),

		// call_expression covers all function/method calls.
		CallTypes: extraction.TypeSet("call_expression"),

		// NameField = "name" — class_specifier, struct_specifier, and enum_specifier
		// all carry a 'name' field (type_identifier). function_definition has no 'name'
		// field, but ResolveBody unwraps it to function_declarator first, whose first
		// named child is the identifier; the fallback name-from-node path handles it.
		NameField: "name",

		// ResolveBody unwraps function_definition to its function_declarator child.
		ResolveBody: cppResolveBody,

		// ResolveKind distinguishes class_specifier / struct_specifier / enum_specifier.
		ResolveKind: cppResolveKind,

		// IsExported: all C++ symbols are treated as exported.
		// C++ access specifiers (public/private/protected) are enforced at usage
		// sites by the compiler; there is no single 'exported' grammar node.
		// Simplification: all = true.
		IsExported: cppIsExported,

		// ExtractImport extracts the header path from preproc_include nodes.
		// Reuse the same logic as C.
		ExtractImport: cExtractInclude,

		// ExtractHeritage extracts base classes from class_specifier / struct_specifier.
		ExtractHeritage: cppExtractHeritage,
	}
}

// cppResolveBody unwraps a function_definition to its function_declarator child,
// so that nameFromNode can find the identifier (function name) as the first
// named child of the declarator. For non-function_definition nodes, returns
// the same node unchanged (class_specifier / struct_specifier already carry
// their name via the 'name' field).
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

// cppResolveKind returns the correct semantic NodeKind for C++ aggregate types:
//
//	class_specifier  → NodeKindClass
//	struct_specifier → NodeKindStruct
//	enum_specifier   → NodeKindEnum
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
		// struct_specifier → NodeKindStruct
		return types.NodeKindStruct
	}
}

// cppIsExported always returns true. C++ does not have a single export keyword;
// visibility is enforced by access specifiers (public/private/protected) at
// usage sites rather than at the declaration site. Treating all top-level
// symbols as exported is the correct simplification for a code-intel graph.
func cppIsExported(_ context.Context, _ sitter.Node, _ string) bool {
	return true
}

// cppExtractHeritage extracts base-class references from C++ class_specifier
// and struct_specifier nodes.
//
// Grammar layout (verified by real parse probe):
//
//	class_specifier
//	  name: type_identifier            — "Circle"
//	  base_class_clause                — ": public Shape, public Drawable"
//	    access_specifier               — "public" (skip)
//	    type_identifier                — "Shape"
//	    access_specifier               — "public" (skip)
//	    type_identifier                — "Drawable"
//
// All type_identifier children of base_class_clause → EdgeKindExtends.
// access_specifier children are skipped.
// C++ does not distinguish extends vs implements at the language level;
// appendix-F promotion will upgrade to EdgeKindImplements when the target
// resolves to an interface/abstract-class node.
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
		// Walk children of base_class_clause.
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
			// Skip access_specifier ("public", "protected", "private").
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
