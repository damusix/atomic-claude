package languages

// Node-type strings are read from the live Erlang grammar — do not guess.
//
// Erlang identity is name plus arity: add/2 and add/3 are different functions.
// Name holds the bare atom, Signature the "name/arity" pair, and the arity is
// the named-child count of the clause's expr_args node.

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

var reLineComment = regexp.MustCompile(`%[^\n]*`)

// Both attribute patterns span [\s\S] so a multi-line list still matches.
var reExportAttr = regexp.MustCompile(`-export\(\[([\s\S]*?)\]\)`)
var reCompileAttr = regexp.MustCompile(`-compile\(([\s\S]*?)\)`)

// reFA matches one "name/arity" token, such as "foo/2".
var reFA = regexp.MustCompile(`([a-z][a-zA-Z0-9_@]*|\d+)/(\d+)`)

// erlangStripComments removes line comments so the attribute patterns below
// cannot match a commented-out attribute.
func erlangStripComments(source string) string {
	return reLineComment.ReplaceAllString(source, "")
}

// erlangExportSet returns the "name/arity" strings the module explicitly
// exports.
func erlangExportSet(source string) map[string]bool {
	stripped := erlangStripComments(source)
	set := make(map[string]bool)
	for _, m := range reExportAttr.FindAllStringSubmatch(stripped, -1) {
		for _, fa := range reFA.FindAllStringSubmatch(m[1], -1) {
			set[fa[0]] = true
		}
	}
	return set
}

// ErlangExtractor returns the LanguageExtractor config for Erlang source files
// (.erl, .hrl).
func ErlangExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		FunctionTypes: extraction.TypeSet("fun_decl"),

		ModuleTypes: extraction.TypeSet("module_attribute"),

		// -record declarations.
		StructTypes: extraction.TypeSet("record_decl"),

		FieldTypes: extraction.TypeSet("record_field"),

		// -define macros. They land as variables because VariableTypes cannot
		// reach NodeKindConstant, which is the closer fit.
		VariableTypes: extraction.TypeSet("pp_define"),

		// export_attribute is deliberately absent: exports are an IsExported
		// question, not an import edge.
		ImportTypes: extraction.TypeSet(
			"behaviour_attribute",
			"import_attribute",
		),

		// "remote" is deliberately unmatched. A remote call, math:sqrt(Z), wraps
		// a remote_module and an inner call; leaving the wrapper unmatched lets
		// the walk descend to that inner call and name the callee "sqrt" rather
		// than the module prefix, which is what the resolution layer can use.
		CallTypes: extraction.TypeSet("call"),

		NameField: "name",
		// Empty on purpose. The body scanner looks for BodyField on the original
		// node, and fun_decl has no body of its own — the body hangs off the
		// inner function_clause. Empty selects the fallback instead, a DFS over
		// the whole fun_decl that reaches the clause body and its calls.
		BodyField: "",

		ResolveBody: erlangResolveBody,

		GetSignature: erlangGetSignature,

		IsExported: erlangIsExported,

		ExtractImport: erlangExtractImport,
	}
}

// erlangResolveBody unwraps a fun_decl to its function_clause and a pp_define to
// its macro_lhs, the nodes that carry the name. Any other node passes through
// unchanged.
func erlangResolveBody(ctx context.Context, node sitter.Node, _ string) (sitter.Node, error) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return node, nil
	}

	switch kind {
	case "fun_decl":
		child, err := node.ChildByFieldName(ctx, "clause")
		if err != nil {
			return node, nil
		}
		isNull, _ := child.IsNull(ctx)
		if isNull {
			cnt, _ := node.NamedChildCount(ctx)
			if cnt == 0 {
				return node, nil
			}
			fc, err := node.NamedChild(ctx, 0)
			if err != nil {
				return node, nil
			}
			return fc, nil
		}
		return child, nil

	case "pp_define":
		cnt, _ := node.NamedChildCount(ctx)
		if cnt == 0 {
			return node, nil
		}
		lhs, err := node.NamedChild(ctx, 0)
		if err != nil {
			return node, nil
		}
		lhsKind, _ := lhs.Kind(ctx)
		if lhsKind != "macro_lhs" {
			return node, nil
		}
		return lhs, nil
	}

	return node, nil
}

// erlangGetSignature builds the "name/arity" identity string for a fun_decl or
// function_clause, and returns "" for anything else.
func erlangGetSignature(ctx context.Context, node sitter.Node, source string) string {
	kind, err := node.Kind(ctx)
	if err != nil {
		return ""
	}

	var clauseNode sitter.Node
	switch kind {
	case "fun_decl":
		child, err := node.ChildByFieldName(ctx, "clause")
		if err != nil {
			return ""
		}
		isNull, _ := child.IsNull(ctx)
		if isNull {
			cnt, _ := node.NamedChildCount(ctx)
			if cnt == 0 {
				return ""
			}
			fc, err := node.NamedChild(ctx, 0)
			if err != nil {
				return ""
			}
			clauseNode = fc
		} else {
			clauseNode = child
		}
	case "function_clause":
		clauseNode = node
	default:
		return ""
	}

	nameChild, err := clauseNode.ChildByFieldName(ctx, "name")
	if err != nil {
		return ""
	}
	isNullName, _ := nameChild.IsNull(ctx)
	if isNullName {
		return ""
	}
	nameSB, _ := nameChild.StartByte(ctx)
	nameEB, _ := nameChild.EndByte(ctx)
	if int(nameEB) > len(source) {
		return ""
	}
	funcName := source[nameSB:nameEB]

	argsChild, err := clauseNode.ChildByFieldName(ctx, "args")
	if err != nil {
		return fmt.Sprintf("%s/0", funcName)
	}
	isNullArgs, _ := argsChild.IsNull(ctx)
	if isNullArgs {
		return fmt.Sprintf("%s/0", funcName)
	}
	arity, _ := argsChild.NamedChildCount(ctx)
	return fmt.Sprintf("%s/%d", funcName, arity)
}

// erlangHasExportAll reports whether the module declares -compile(export_all),
// which exports every function in it. The check is anchored to the -compile
// attribute rather than searching the source, so the atom appearing elsewhere
// cannot trigger it.
func erlangHasExportAll(source string) bool {
	stripped := erlangStripComments(source)
	for _, m := range reCompileAttr.FindAllStringSubmatch(stripped, -1) {
		if strings.Contains(m[1], "export_all") {
			return true
		}
	}
	return false
}

// erlangIsExported reports whether a node is part of the module's public
// interface. Module and record declarations always are. A function is exported
// only if its name/arity appears in a parsed -export list, never by a substring
// search of the source, which would also hit a "fun name/arity" reference or a
// -spec annotation.
func erlangIsExported(ctx context.Context, node sitter.Node, source string) bool {
	kind, err := node.Kind(ctx)
	if err != nil {
		return false
	}
	switch kind {
	case "module_attribute", "record_decl":
		return true
	case "fun_decl":
		if erlangHasExportAll(source) {
			return true
		}
		fc, err := erlangResolveBody(ctx, node, source)
		if err != nil {
			return false
		}
		nameChild, err := fc.ChildByFieldName(ctx, "name")
		if err != nil {
			return false
		}
		isNull, _ := nameChild.IsNull(ctx)
		if isNull {
			return false
		}
		nameSB, _ := nameChild.StartByte(ctx)
		nameEB, _ := nameChild.EndByte(ctx)
		if int(nameEB) > len(source) {
			return false
		}
		funcName := source[nameSB:nameEB]

		argsChild, err := fc.ChildByFieldName(ctx, "args")
		if err != nil {
			return false
		}
		isNullArgs, _ := argsChild.IsNull(ctx)
		if isNullArgs {
			return false
		}
		arity, _ := argsChild.NamedChildCount(ctx)
		target := fmt.Sprintf("%s/%d", funcName, arity)

		return erlangExportSet(source)[target]
	}
	return false
}

// erlangExtractImport returns the module named by an import_attribute or
// behaviour_attribute as both the name and the path.
func erlangExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return "", ""
	}

	switch kind {
	case "behaviour_attribute":
		nameChild, err := node.ChildByFieldName(ctx, "name")
		if err != nil {
			return "", ""
		}
		isNull, _ := nameChild.IsNull(ctx)
		if isNull {
			return "", ""
		}
		sb, _ := nameChild.StartByte(ctx)
		eb, _ := nameChild.EndByte(ctx)
		if int(eb) > len(source) {
			return "", ""
		}
		modName := source[sb:eb]
		return modName, modName

	case "import_attribute":
		// The imported function list is dropped; only the module is recorded.
		modChild, err := node.ChildByFieldName(ctx, "module")
		if err != nil {
			return "", ""
		}
		isNull, _ := modChild.IsNull(ctx)
		if isNull {
			return "", ""
		}
		sb, _ := modChild.StartByte(ctx)
		eb, _ := modChild.EndByte(ctx)
		if int(eb) > len(source) {
			return "", ""
		}
		modName := source[sb:eb]
		return modName, modName
	}

	return "", ""
}
