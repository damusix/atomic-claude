package languages

// Java language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-cp8a/ — Java grammar):
//
//	Top-level (direct children of program):
//	  import_declaration       — import java.util.List;
//	  interface_declaration    — public interface Drawable { ... }
//	  enum_declaration         — public enum Direction { ... }
//	  class_declaration        — public class Canvas ... / public class Main ...
//
//	Named-iterator also sees (inside bodies):
//	  method_declaration       — public void draw() {} / int getId() {}
//	  field_declaration        — private int id; / public String name;
//	  method_invocation        — render(this.id) / c.draw() / System.out.println(...)
//	  object_creation_expression — new Canvas(1, "test")
//
// Name field: 'name' works on class_declaration, interface_declaration,
// enum_declaration, method_declaration (verified by probe4).
//
// IsExported rule: 'modifiers' named child (kind="modifiers") contains "public".
// Java uses plural 'modifiers' as a container node — distinct from C# 'modifier'.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// JavaExtractor returns the LanguageExtractor config for Java source files (.java).
//
// Node-type strings are verified by parsing real Java via the wazero binding
// (see tmp/probe-cp8a/main.go and tmp/probe-cp8a/probe4.go).
func JavaExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// Java has method_declaration for methods; no separate function_declaration.
		MethodTypes: extraction.TypeSet("method_declaration"),

		// Separate top-level declarations — all have 'name' field.
		ClassTypes:     extraction.TypeSet("class_declaration"),
		InterfaceTypes: extraction.TypeSet("interface_declaration"),
		EnumTypes:      extraction.TypeSet("enum_declaration"),

		// Field and import declarations.
		FieldTypes:  extraction.TypeSet("field_declaration"),
		ImportTypes: extraction.TypeSet("import_declaration"),

		// method_invocation for regular calls; object_creation_expression for new Foo().
		CallTypes:          extraction.TypeSet("method_invocation"),
		InstantiationTypes: extraction.TypeSet("object_creation_expression"),

		// Field names in the Java grammar (verified by probe4).
		NameField:   "name",
		BodyField:   "body",
		ParamsField: "formal_parameters",

		// IsExported: checks for a 'modifiers' named child (kind="modifiers") whose
		// text contains "public". Java uses a container 'modifiers' node (plural),
		// distinct from C#'s 'modifier' (singular).
		IsExported: javaIsExported,

		// ExtractImport extracts the import path from import_declaration nodes.
		ExtractImport: javaExtractImport,

		// ExtractHeritage extracts superclass and implemented interfaces from
		// class_declaration nodes.
		ExtractHeritage: javaExtractHeritage,
	}
}

// javaIsExported reports whether a Java node is public.
// Rules:
//  1. If the node has a 'modifiers' child (kind="modifiers") containing "public" → exported.
//  2. If the node is a method_declaration with NO 'modifiers' child AND no body →
//     it is an interface abstract method (implicitly public in Java) → exported.
//  3. Otherwise → not exported (package-private or non-public modifier).
//
// Rule 2 is scoped to method_declaration only. A bare field_declaration with no
// modifiers is package-private in Java, not public — applying the implicit-public
// fallback to fields would wrongly grant them IsExported=true and skew resolution
// scoring (+10 ScoreExported) toward hidden symbols.
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
	// Interface abstract method: method_declaration with no modifiers and no body
	// is implicitly public in Java. Scope this rule to method_declaration only —
	// a bare field_declaration with no modifiers is package-private, not public.
	if nodeKind == "method_declaration" && !hasModifiers && !hasBlock {
		return true
	}
	return false
}

// javaExtractImport extracts the import path from an import_declaration node.
//
//	import java.util.List;         → name="List",  path="java.util.List"
//	import java.io.*;              → name="io",    path="java.io"
//	import static java.lang.Math.PI; → name="PI",  path="java.lang.Math.PI"
func javaExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "import_declaration" {
		return "", ""
	}

	// import_declaration's first named child is typically a scoped_identifier or
	// asterisk. Use the full node text and strip "import " prefix and ";".
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

	// Handle wildcard imports: "java.io.*" → path="java.io", name="io".
	if strings.HasSuffix(text, ".*") {
		path = strings.TrimSuffix(text, ".*")
	} else {
		path = text
	}

	// Name = last segment.
	segments := strings.Split(path, ".")
	name = segments[len(segments)-1]
	return name, path
}

// javaExtractHeritage extracts superclass and implemented-interface references
// from Java class_declaration nodes.
//
// Grammar layout (verified by real parse probe):
//
//	class_declaration
//	  name: identifier                 — "Dog"
//	  superclass                       — "extends Animal"
//	    type_identifier                — "Animal"
//	  super_interfaces                 — "implements Speakable, Runnable"
//	    type_list
//	      type_identifier              — "Speakable"
//	      type_identifier              — "Runnable"
//
// superclass → first type_identifier child → EdgeKindExtends.
// super_interfaces → type_list → all type_identifier grandchildren → EdgeKindImplements.
// interface_declaration has no superclass / super_interfaces in Java 8+ grammar.
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
			// superclass has one type_identifier child.
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
			// super_interfaces → type_list → type_identifier grandchildren.
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
