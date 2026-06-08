package languages

// Pascal language extractor configuration.
//
// Verified node-type strings (parsed via tmp/probe-cp8d/ — Pascal grammar):
//
//	Top-level / interface section:
//	  unit          — "unit Canvas;\n interface\n..."
//	  declUses      — "uses SysUtils, Classes;"
//	  declTypes     — wrapper for type declaration block
//	  declType      — "TDirection = (...);" / "TShape = class(...)" / "IDrawable = interface..."
//	                  dispatched to ResolveKind to classify as class/interface/enum
//	  declProc      — "procedure Draw; virtual;" / "function GetId: Integer;"
//	                  also constructor Create / destructor Destroy in interface section
//
//	Implementation section:
//	  defProc       — "procedure TShape.Draw;\nbegin ... end;"
//	                  wraps a declProc child; ResolveBody unwraps it for name extraction
//
//	Type declaration children (children of declType):
//	  declClass     — "class(TObject, IDrawable) ... end"  → NodeKindClass
//	  declIntf      — "interface ... end"                  → NodeKindInterface
//	  declEnum      — "(dNorth, dSouth, dEast, dWest)"     → NodeKindEnum
//
//	Call expressions:
//	  exprCall      — "Render(FId)" / "WriteLn(FName)" / "TShape.Create(AId, AName)"
//
//	Import nodes:
//	  declUses      — each module listed as a moduleName child containing an identifier
//
// Name extraction:
//   - declType: first identifier named child = type name (e.g. "TShape", "TDirection")
//   - declProc: first identifier named child after proc/func keyword = proc name
//   - defProc: contains declProc child — ResolveBody unwraps it; name from inner declProc
//   - exprCall: first identifier named child = callee name
//
// ResolveKind for declType:
//   - Has "declClass" named child → NodeKindClass
//   - Has "declIntf" named child  → NodeKindInterface
//   - Has "declEnum" named child  → NodeKindEnum
//   - Otherwise                   → NodeKindClass (fallback)
//
// IsExported rule: Pascal has public/private/protected section keywords inside
// class definitions (declSection with kPublic/kPrivate child), but these are
// structural section markers rather than per-symbol modifiers. CP8 does not
// track class section context — all Pascal symbols are treated as exported.
// Per-section access control would require parent-section accumulation (future work).

import (
	"context"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
	"github.com/damusix/atomic-claude/atomic/internal/codeintel/types"
)

// PascalExtractor returns the LanguageExtractor config for Pascal source files (.pas, .pp).
//
// Node-type strings are verified by parsing real Pascal via the wazero binding
// (see tmp/probe-cp8d/main.go).
func PascalExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// declProc covers all procedure/function/constructor/destructor declarations
		// in the interface section.
		// defProc covers the corresponding implementations in the implementation section.
		// defProc wraps a declProc child; ResolveBody unwraps it.
		FunctionTypes: extraction.TypeSet("declProc", "defProc"),

		// declType is the Pascal type declaration node. It is dispatched to
		// ResolveKind which inspects its children to determine the correct kind:
		// class → NodeKindClass, interface → NodeKindInterface, enum → NodeKindEnum.
		// Placed in StructTypes so the engine calls ResolveKind automatically.
		StructTypes: extraction.TypeSet("declType"),

		// declUses covers "uses SysUtils, Classes;" — the Pascal import mechanism.
		ImportTypes: extraction.TypeSet("declUses"),

		// exprCall covers function/procedure call expressions.
		CallTypes: extraction.TypeSet("exprCall"),

		// No NameField: the grammar does not use a uniform "name" field for Pascal nodes.
		// nameFromNode's identifier fallback scan finds names correctly.
		NameField: "",

		// ResolveBody unwraps defProc → inner declProc for name extraction.
		// defProc is the implementation-side procedure node; its name is held
		// by its inner declProc child (which carries "TShape.Draw" etc.).
		ResolveBody: pascalResolveBody,

		// ResolveKind disambiguates declType into class/interface/enum.
		ResolveKind: pascalResolveKind,

		// IsExportedByName: Pascal symbols are public by default.
		// Return true unconditionally (see IsExported rule comment above).
		IsExportedByName: pascalIsExportedByName,

		// ExtractImport: extract each module name from a declUses node.
		// A uses clause lists multiple modules; we return the first one found.
		ExtractImport: pascalExtractImport,
	}
}

// pascalResolveBody unwraps defProc → the inner declProc node.
//
// In the Pascal grammar, defProc is the implementation-section procedure
// node. It wraps a declProc child which carries the qualified name
// (e.g. "TShape.Draw") as an identifier child. Without this unwrap,
// nameFromNode would look at defProc's direct children and find declProc
// rather than an identifier — causing extraction to produce the wrong name
// or empty name.
//
// For any other node type, the original node is returned unchanged.
func pascalResolveBody(ctx context.Context, node sitter.Node, _ string) (sitter.Node, error) {
	kind, err := node.Kind(ctx)
	if err != nil || kind != "defProc" {
		return node, nil
	}
	// Find the declProc named child.
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

// pascalResolveKind disambiguates a declType node into the correct semantic kind.
//
//	Has "declClass" named child → NodeKindClass   (e.g. "TShape = class(TObject)")
//	Has "declIntf"  named child → NodeKindInterface (e.g. "IDrawable = interface")
//	Has "declEnum"  named child → NodeKindEnum    (e.g. "TDirection = (dNorth,...)")
//	Otherwise                   → NodeKindClass   (fallback for unrecognized form)
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

// pascalIsExportedByName reports that all Pascal symbols are exported (public).
//
// Pascal's public/private/protected are class-section markers (not per-symbol
// modifiers). Tracking section context is out of scope for CP8; all symbols
// are treated as public (exported).
func pascalIsExportedByName(_ string) bool {
	return true
}

// pascalExtractImport extracts the first module name from a declUses node.
//
// Pascal grammar structure:
//
//	declUses
//	  moduleName   ← one per imported module
//	    identifier   ← module name text (e.g. "SysUtils", "Classes")
//
// A single declUses clause may list several modules; ExtractImport returns only
// the first one. The engine calls ExtractImport once per import node — multiple
// modules in one uses clause are a grammar-level constraint that would require
// multi-value emit (not supported by the current ExtractImport contract).
func pascalExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	// Find first moduleName named child.
	modName, ok := firstNamedChildOfKind(ctx, node, "moduleName")
	if !ok {
		return "", ""
	}
	// Find identifier inside moduleName.
	ident, ok := firstNamedChildOfKind(ctx, modName, "identifier")
	if !ok {
		// Fallback: use moduleName text directly.
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

// Ensure pascalIsExportedByName satisfies the IsExportedByName signature.
var _ func(string) bool = pascalIsExportedByName

// Ensure pascalExtractImport satisfies the ExtractImport signature.
var _ func(context.Context, sitter.Node, string) (string, string) = pascalExtractImport

// Ensure pascalResolveBody satisfies the ResolveBody signature.
var _ func(context.Context, sitter.Node, string) (sitter.Node, error) = pascalResolveBody

// Ensure pascalResolveKind satisfies the ResolveKind signature.
var _ func(context.Context, sitter.Node, string) types.NodeKind = pascalResolveKind
