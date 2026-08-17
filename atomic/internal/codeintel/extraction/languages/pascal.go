package languages

// Node-type strings are read from the live Pascal grammar — do not guess. This
// grammar's camelCase names (declProc, defProc, declType) are its own; nothing
// else in this package looks like them. No node type carries a "name" field, so
// this config leaves NameField empty and relies on the identifier-scanning
// fallback.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// PascalExtractor returns the LanguageExtractor config for Pascal source files (.pas, .pp).
func PascalExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// Interface-section declarations and their implementation-section
		// definitions, procedures and constructors alike.
		FunctionTypes: extraction.TypeSet("declProc", "defProc"),

		// Class, interface, and enum share this node type; ResolveKind splits
		// them. StructTypes is the arm that runs it.
		StructTypes: extraction.TypeSet("declType"),

		// A uses clause is Pascal's import.
		ImportTypes: extraction.TypeSet("declUses"),

		CallTypes: extraction.TypeSet("exprCall"),

		NameField: "",

		ResolveBody: pascalResolveBody,

		ResolveKind: pascalResolveKind,

		IsExportedByName: pascalIsExportedByName,

		ExtractImport: pascalExtractImport,
	}
}

// pascalResolveBody unwraps a defProc to the declProc it holds, which is what
// carries the qualified name ("TShape.Draw"); defProc's own children include no
// identifier. Any other node passes through unchanged.
func pascalResolveBody(ctx context.Context, node sitter.Node, _ string) (sitter.Node, error) {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "defProc" {
		return node, nil
	}
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return node, nil
	}
	for i := uint64(0); i < cnt; i++ {
		ch, err := node.NamedChild(ctx, i)
		if err != nil {
			continue
		}
		ck, err := ch.Kind(ctx)
		if err != nil {
			continue
		}
		if ck == "declProc" {
			return ch, nil
		}
	}
	return node, nil
}

// pascalResolveKind reads the child that says what a declType declares. An
// unrecognized form falls back to a class.
func pascalResolveKind(ctx context.Context, node sitter.Node, _ string) types.NodeKind {
	cnt, err := node.NamedChildCount(ctx)
	if err != nil {
		return types.NodeKindClass
	}
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
		case "declClass":
			return types.NodeKindClass
		case "declIntf":
			return types.NodeKindInterface
		case "declEnum":
			return types.NodeKindEnum
		}
	}
	return types.NodeKindClass
}

// pascalIsExportedByName always reports true: public, private, and protected
// mark class sections in Pascal, not individual symbols, and honoring them would
// take a parent walk the framework does not offer.
func pascalIsExportedByName(_ string) bool {
	return true
}

// pascalExtractImport returns the first module of a uses clause and nothing
// more: ExtractImport returns one name and path, while a clause may list many.
func pascalExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	modName, ok := firstNamedChildOfKind(ctx, node, "moduleName")
	if !ok {
		return "", ""
	}
	ident, ok := firstNamedChildOfKind(ctx, modName, "identifier")
	if !ok {
		ident = modName
	}
	sb, _ := ident.StartByte(ctx)
	eb, _ := ident.EndByte(ctx)
	if int(eb) > len(source) {
		return "", ""
	}
	raw := strings.TrimSpace(source[sb:eb])
	if raw == "" {
		return "", ""
	}
	return raw, raw
}

// Compile-time checks that each hook still matches its LanguageExtractor field.
var _ func(string) bool = pascalIsExportedByName
var _ func(context.Context, sitter.Node, string) (string, string) = pascalExtractImport
var _ func(context.Context, sitter.Node, string) (sitter.Node, error) = pascalResolveBody
var _ func(context.Context, sitter.Node, string) types.NodeKind = pascalResolveKind
