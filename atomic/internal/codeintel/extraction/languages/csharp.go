package languages

// C# language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-cp8a/ — C# grammar):
//
//	Top-level (direct children of compilation_unit / namespace_declaration body):
//	  using_directive          — using System; / using System.Collections.Generic;
//	  namespace_declaration    — namespace MyApp { ... }
//	  interface_declaration    — public interface IDrawable { ... }
//	  enum_declaration         — public enum Direction { ... }
//	  class_declaration        — public class Canvas : IDrawable { ... }
//
//	Named-iterator also sees (inside class bodies):
//	  method_declaration       — public void Draw() {} / private static void Render(...)
//	  property_declaration     — public string Name { get; set; }
//	  field_declaration        — private int _id; / protected List<string> Items;
//	  invocation_expression    — Render(_id) / Console.WriteLine(...)
//	  object_creation_expression — new Canvas(1, "test") / new List<string>()
//
// IMPORTANT: C# uses 'invocation_expression' (NOT 'call_expression') for method
// calls. Wrong node type = zero call references. Verified by probe.
//
// Name field: 'name' works on class_declaration, interface_declaration,
// enum_declaration, method_declaration (verified by probe3 and probe4).
//
// IsExported rule: 'modifier' named child (kind="modifier", singular) whose
// text is "public". C# uses singular 'modifier' repeated per modifier —
// distinct from Java's plural 'modifiers' container node.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// CSharpExtractor returns the LanguageExtractor config for C# source files (.cs).
//
// Node-type strings are verified by parsing real C# via the wazero binding
// (see tmp/probe-cp8a/main.go, probe2.go, probe3.go, probe4.go).
func CSharpExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// method_declaration covers all named methods (including constructors use
		// constructor_declaration — out of scope for this checkpoint).
		MethodTypes: extraction.TypeSet("method_declaration"),

		// Separate top-level declarations — all have 'name' field.
		ClassTypes:     extraction.TypeSet("class_declaration"),
		InterfaceTypes: extraction.TypeSet("interface_declaration"),
		EnumTypes:      extraction.TypeSet("enum_declaration"),

		// property_declaration covers auto-properties (Name { get; set; }).
		PropertyTypes: extraction.TypeSet("property_declaration"),

		// field_declaration covers plain fields.
		FieldTypes: extraction.TypeSet("field_declaration"),

		// using_directive is C#'s import mechanism.
		ImportTypes: extraction.TypeSet("using_directive"),

		// invocation_expression for method calls (NOT call_expression — C# grammar
		// uses invocation_expression). object_creation_expression for new Foo().
		CallTypes:          extraction.TypeSet("invocation_expression"),
		InstantiationTypes: extraction.TypeSet("object_creation_expression"),

		// Field names in the C# grammar (verified by probe3 and probe4).
		NameField:   "name",
		BodyField:   "body",
		ParamsField: "parameter_list",

		// IsExported: checks for a 'modifier' named child (kind="modifier") whose
		// text is "public". C# uses individual 'modifier' nodes per modifier keyword.
		IsExported: csharpIsExported,

		// ExtractImport extracts the namespace path from using_directive nodes.
		ExtractImport: csharpExtractImport,
	}
}

// csharpIsExported reports whether a C# node is public.
// Rules:
//  1. If the node has a 'modifier' child (kind="modifier") with text "public" → exported.
//  2. If the node is a method_declaration with NO 'modifier' children AND no body →
//     it is an interface abstract method (implicitly public in C#) → exported.
//  3. Otherwise → not exported.
//
// Rule 2 is scoped to method_declaration only. A bare field_declaration with no
// modifiers defaults to private in C#, not public — applying the implicit-public
// fallback to fields would wrongly grant them IsExported=true and skew resolution
// scoring (+10 ScoreExported) toward private symbols.
func csharpIsExported(ctx context.Context, node sitter.Node, source string) bool {
	nodeKind, err := node.Kind(ctx)
	if err != nil {
		return false
	}
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return false
	}
	hasModifier := false
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
		case "modifier":
			hasModifier = true
			sb, _ := ch.StartByte(ctx)
			eb, _ := ch.EndByte(ctx)
			if int(eb) <= len(source) {
				text := strings.TrimSpace(source[sb:eb])
				if text == "public" {
					return true
				}
			}
		case "block":
			hasBlock = true
		}
	}
	// Interface abstract method: method_declaration with no modifiers and no body
	// is implicitly public in C#. Scope this rule to method_declaration only —
	// a bare field_declaration with no modifiers is private by default in C#.
	if nodeKind == "method_declaration" && !hasModifier && !hasBlock {
		return true
	}
	return false
}

// csharpExtractImport extracts the namespace path from a using_directive node.
//
//	using System;                         → name="System",     path="System"
//	using System.Collections.Generic;     → name="Generic",    path="System.Collections.Generic"
//	using static System.Math;             → name="Math",       path="System.Math"
func csharpExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "using_directive" {
		return "", ""
	}

	sb, _ := node.StartByte(ctx)
	eb, _ := node.EndByte(ctx)
	if int(eb) > len(source) {
		return "", ""
	}
	text := strings.TrimSpace(source[sb:eb])
	// Strip "using " prefix, optional "static ", trailing ";".
	text = strings.TrimPrefix(text, "using ")
	text = strings.TrimPrefix(text, "static ")
	text = strings.TrimSuffix(text, ";")
	text = strings.TrimSpace(text)

	if text == "" {
		return "", ""
	}

	path = text
	// Name = last segment.
	segments := strings.Split(path, ".")
	name = segments[len(segments)-1]
	return name, path
}
