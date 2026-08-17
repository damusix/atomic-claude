package languages

// Node-type strings are read from the live Dart grammar — do not guess. No node
// type carries a "name" field, so this config leaves NameField empty throughout
// and relies on the identifier-scanning fallback.
//
// Calls cannot be extracted at all: the grammar has no call node: a call site is
// an expression_statement holding an identifier and a selector, and the walk has
// nothing to match on. TestDart_CallsBlocked pins that constraint.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// DartExtractor returns the LanguageExtractor config for Dart source files (.dart).
func DartExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// Methods inside a class body are reached by descent into the same
		// function_signature node type.
		FunctionTypes: extraction.TypeSet("function_signature", "constructor_signature"),

		// Mixins join classes: Dart has no separate interface construct.
		ClassTypes: extraction.TypeSet("class_definition", "mixin_declaration"),

		EnumTypes: extraction.TypeSet("enum_declaration"),

		// Covers export directives too.
		ImportTypes: extraction.TypeSet("import_or_export"),

		// CallTypes stays empty; see the file header.

		NameField: "",

		ResolveBody: dartResolveBody,

		// Dart marks visibility by name alone, so no AST walk is needed.
		IsExportedByName: dartIsExportedByName,

		ExtractImport: dartExtractImport,
	}
}

// dartResolveBody points a function_signature at its name identifier. Without
// it, the identifier-scanning fallback would return the return type instead,
// which precedes the name and is itself a type_identifier. The name is the
// identifier immediately before the parameter list. Any other node, constructors
// included, passes through unchanged and scans correctly.
func dartResolveBody(ctx context.Context, node sitter.Node, _ string) (sitter.Node, error) {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "function_signature" {
		return node, nil
	}
	cnt, err := node.NamedChildCount(ctx)
	if err != nil || cnt == 0 {
		return node, nil
	}
	var lastIdentifier sitter.Node
	foundIdent := false
	for i := uint64(0); i < cnt; i++ {
		ch, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		ck, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		if ck == "formal_parameter_list" {
			if foundIdent {
				return lastIdentifier, nil
			}
			break
		}
		if ck == "identifier" {
			lastIdentifier = ch
			foundIdent = true
		}
	}
	return node, nil
}

// dartIsExportedByName applies Dart's leading-underscore privacy convention.
func dartIsExportedByName(name string) bool {
	return !strings.HasPrefix(name, "_")
}

// dartExtractImport descends to the string literal holding the URI and returns
// it as the path, with its last segment as the name.
func dartExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	cur := node
	for _, targetKind := range []string{
		"library_import",
		"import_specification",
		"configurable_uri",
		"uri",
	} {
		next, ok := firstNamedChildOfKind(ctx, cur, targetKind)
		if !ok {
			return "", ""
		}
		cur = next
	}

	strLit, ok := firstNamedChildOfKind(ctx, cur, "string_literal")
	if !ok {
		strLit = cur
	}

	sb, _ := strLit.StartByte(ctx)
	eb, _ := strLit.EndByte(ctx)
	if int(eb) > len(source) {
		return "", ""
	}
	raw := strings.TrimSpace(source[sb:eb])
	raw = strings.Trim(raw, `'"`)
	if raw == "" {
		return "", ""
	}
	path = raw
	// A scheme separator ends a segment too: "dart:async" → "async".
	segments := strings.FieldsFunc(path, func(r rune) bool { return r == '/' || r == ':' })
	if len(segments) > 0 {
		name = segments[len(segments)-1]
	} else {
		name = path
	}
	return name, path
}

// firstNamedChildOfKind reports the first named child of node with the given
// kind, and whether one was found. Shared with the Pascal import extractor.
func firstNamedChildOfKind(ctx context.Context, node sitter.Node, targetKind string) (sitter.Node, bool) {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return sitter.Node{}, false
	}
	for i := uint64(0); i < cnt; i++ {
		ch, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		k, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		if k == targetKind {
			return ch, true
		}
	}
	return sitter.Node{}, false
}

// Compile-time checks that each hook still matches its LanguageExtractor field.
var _ func(string) bool = dartIsExportedByName
var _ func(context.Context, sitter.Node, string) (string, string) = dartExtractImport
var _ func(context.Context, sitter.Node, string) (sitter.Node, error) = dartResolveBody
