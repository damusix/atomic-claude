package languages

// Node-type strings are read from the live Java grammar — do not guess. Java
// puts every modifier inside one plural "modifiers" container node, where C#
// repeats a singular "modifier" node; the two extractors read them differently
// for that reason.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// JavaExtractor returns the LanguageExtractor config for Java source files (.java).
func JavaExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// Java has no free functions, so every callable is a method.
		MethodTypes: extraction.TypeSet("method_declaration"),

		ClassTypes:     extraction.TypeSet("class_declaration"),
		InterfaceTypes: extraction.TypeSet("interface_declaration"),
		EnumTypes:      extraction.TypeSet("enum_declaration"),

		FieldTypes:  extraction.TypeSet("field_declaration"),
		ImportTypes: extraction.TypeSet("import_declaration"),

		CallTypes:          extraction.TypeSet("method_invocation"),
		InstantiationTypes: extraction.TypeSet("object_creation_expression"),

		NameField:   "name",
		BodyField:   "body",
		ParamsField: "formal_parameters",

		IsExported: javaIsExported,

		ExtractImport: javaExtractImport,

		ExtractHeritage: javaExtractHeritage,
	}
}

// javaIsExported looks for "public" in the modifiers container, then falls back
// to treating a bodiless, modifier-less method as an implicitly public interface
// method. That fallback is deliberately scoped to methods: an unmarked field is
// package-private, and calling it exported would hand hidden symbols the
// exported bonus in resolution scoring.
func javaIsExported(ctx context.Context, node sitter.Node, source string) bool {
	nodeKind, err := node.Kind(ctx)
	if err != nil {
		return false
	}
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return false
	}
	hasModifiers := false
	hasBlock := false
	for i := uint64(0); i < cnt; i++ {
		ch, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		kind, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		switch kind {
		case "modifiers":
			hasModifiers = true
			sb, _ := ch.StartByte(ctx)
			eb, _ := ch.EndByte(ctx)
			if int(eb) <= len(source) {
				text := source[sb:eb]
				if strings.Contains(text, "public") {
					return true
				}
			}
		case "block", "constructor_body":
			hasBlock = true
		}
	}
	if nodeKind == "method_declaration" && !hasModifiers && !hasBlock {
		return true
	}
	return false
}

// javaExtractImport returns the import path and its last segment as the name. A
// wildcard import yields the package it expands: "java.io.*" → "java.io".
func javaExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "import_declaration" {
		return "", ""
	}

	sb, _ := node.StartByte(ctx)
	eb, _ := node.EndByte(ctx)
	if int(eb) > len(source) {
		return "", ""
	}
	text := strings.TrimSpace(source[sb:eb])
	text = strings.TrimPrefix(text, "import ")
	text = strings.TrimPrefix(text, "static ")
	text = strings.TrimSuffix(text, ";")
	text = strings.TrimSpace(text)

	if strings.HasSuffix(text, ".*") {
		path = strings.TrimSuffix(text, ".*")
	} else {
		path = text
	}

	segments := strings.Split(path, ".")
	name = segments[len(segments)-1]
	return name, path
}

// javaExtractHeritage reads a class_declaration's superclass child as an extends
// edge and every type_identifier under its super_interfaces/type_list as an
// implements edge. Only classes are handled: the Java 8+ grammar gives
// interface_declaration neither child.
func javaExtractHeritage(ctx context.Context, node sitter.Node, source string) []extraction.HeritageRef {
	kind, err := node.Kind(ctx)
	if err != nil {
		return nil
	}
	if kind != "class_declaration" {
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
		switch ck {
		case "superclass":
			gcnt, err := ch.NamedChildCount(ctx)
			if err != nil {
				continue
			}
			for j := uint64(0); j < gcnt; j++ {
				gch, err := ch.NamedChild(ctx, j)
				if err != nil {
					continue
				}
				gck, err := gch.Kind(ctx)
				if err != nil {
					continue
				}
				if gck != "type_identifier" {
					continue
				}
				sb, _ := gch.StartByte(ctx)
				eb, _ := gch.EndByte(ctx)
				if int(eb) > len(source) {
					continue
				}
				name := strings.TrimSpace(source[sb:eb])
				if name != "" {
					refs = append(refs, extraction.HeritageRef{Name: name, Kind: types.EdgeKindExtends})
				}
				break // only one superclass in Java
			}
		case "super_interfaces":
			slcnt, err := ch.NamedChildCount(ctx)
			if err != nil {
				continue
			}
			for j := uint64(0); j < slcnt; j++ {
				typeListNode, err := ch.NamedChild(ctx, j)
				if err != nil {
					continue
				}
				tlk, err := typeListNode.Kind(ctx)
				if err != nil || tlk != "type_list" {
					continue
				}
				tlcnt, err := typeListNode.NamedChildCount(ctx)
				if err != nil {
					continue
				}
				for k := uint64(0); k < tlcnt; k++ {
					tch, err := typeListNode.NamedChild(ctx, k)
					if err != nil {
						continue
					}
					tck, err := tch.Kind(ctx)
					if err != nil {
						continue
					}
					if tck != "type_identifier" {
						continue
					}
					sb, _ := tch.StartByte(ctx)
					eb, _ := tch.EndByte(ctx)
					if int(eb) > len(source) {
						continue
					}
					name := strings.TrimSpace(source[sb:eb])
					if name != "" {
						refs = append(refs, extraction.HeritageRef{Name: name, Kind: types.EdgeKindImplements})
					}
				}
			}
		}
	}
	return refs
}
