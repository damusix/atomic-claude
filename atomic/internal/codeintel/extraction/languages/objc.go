package languages

// Node-type strings are read from the live ObjC grammar — do not guess. No node
// type carries a uniform "name" field, so this config leaves NameField empty and
// relies on the identifier-scanning fallback, with ResolveBody steering it past
// the two shapes it gets wrong.

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// ObjCExtractor returns the LanguageExtractor config for Objective-C source files (.m, .h).
func ObjCExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		ClassTypes: extraction.TypeSet("class_interface", "class_implementation"),

		// Protocols are ObjC's interface equivalent.
		InterfaceTypes: extraction.TypeSet("protocol_declaration"),

		// C-style free functions, plus the method implementations inside an
		// @implementation block.
		FunctionTypes: extraction.TypeSet("function_definition", "implementation_definition"),

		// The bodiless declarations in @interface and @protocol.
		MethodTypes: extraction.TypeSet("method_declaration"),

		// Covers #import as well as #include.
		ImportTypes: extraction.TypeSet("preproc_include"),

		// Message sends and C-style calls both count.
		CallTypes: extraction.TypeSet("message_expression", "call_expression"),

		NameField: "",

		ResolveBody: objcResolveBody,

		IsExportedByName: objcIsExportedByName,

		ExtractImport: objcExtractImport,
	}
}

// objcResolveBody steers the identifier-scanning fallback past the two node
// shapes where it lands on the wrong thing: an implementation_definition, whose
// own children hold no identifier, and a function_definition, whose first
// identifier-like child is the return type. Any other node passes through
// unchanged.
func objcResolveBody(ctx context.Context, node sitter.Node, _ string) (sitter.Node, error) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return node, nil
	}

	switch kind {
	case "implementation_definition":
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
			if ck == "method_definition" {
				return ch, nil
			}
		}

	case "function_definition":
		ptrDecl, ok := firstNamedChildOfKind(ctx, node, "pointer_declarator")
		if !ok {
			// Non-pointer return type: no pointer_declarator to descend through.
			fnDecl, ok2 := firstNamedChildOfKind(ctx, node, "function_declarator")
			if !ok2 {
				return node, nil
			}
			ident, ok3 := firstNamedChildOfKind(ctx, fnDecl, "identifier")
			if !ok3 {
				return node, nil
			}
			return ident, nil
		}
		fnDecl, ok := firstNamedChildOfKind(ctx, ptrDecl, "function_declarator")
		if !ok {
			return node, nil
		}
		ident, ok := firstNamedChildOfKind(ctx, fnDecl, "identifier")
		if !ok {
			return node, nil
		}
		return ident, nil
	}

	return node, nil
}

// objcIsExportedByName always reports true: ObjC has no method-level access
// modifier. @private and @protected mark ivar sections, not methods.
func objcIsExportedByName(_ string) bool {
	return true
}

// objcExtractImport returns the header path and its last segment as the name.
// Angle-bracket and quoted forms are treated alike.
func objcExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	sb, _ := node.StartByte(ctx)
	eb, _ := node.EndByte(ctx)
	if int(eb) > len(source) {
		return "", ""
	}
	raw := strings.TrimSpace(source[sb:eb])
	for _, prefix := range []string{"#import ", "#include "} {
		raw = strings.TrimPrefix(raw, prefix)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	if strings.HasPrefix(raw, "<") && strings.HasSuffix(raw, ">") {
		raw = raw[1 : len(raw)-1]
	} else if strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) {
		raw = raw[1 : len(raw)-1]
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	path = raw
	segments := strings.Split(path, "/")
	name = segments[len(segments)-1]
	return name, path
}

// Compile-time checks that each hook still matches its LanguageExtractor field.
var _ func(string) bool = objcIsExportedByName
var _ func(context.Context, sitter.Node, string) (string, string) = objcExtractImport
var _ func(context.Context, sitter.Node, string) (sitter.Node, error) = objcResolveBody

var _ = types.NodeKindFunction
