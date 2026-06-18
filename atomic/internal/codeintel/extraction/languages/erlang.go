package languages

// Erlang language extractor configuration.
//
// Verified node-type strings (probed from the live Erlang grammar via the wazero
// binding — see tmp/probe-erlang/main.go and tmp/probe-erlang/fields.go):
//
// Top-level grammar nodes (direct children of source_file):
//
//	module_attribute    — -module(Name).    → module symbol
//	behaviour_attribute — -behaviour(Mod).  → import edge (implements reference)
//	export_attribute    — -export([f/N, …]). → import edge (tracks exported fns)
//	import_attribute    — -import(Mod, […]). → import edge
//	record_decl         — -record(Name, {fields}). → struct symbol
//	record_field        — a field inside record_decl → field symbol
//	pp_define           — -define(NAME, Value). → variable (constant) symbol
//	fun_decl            — FuncName(Args) -> Body. → function symbol
//	spec                — -spec …  (skipped — type metadata only)
//
// Erlang identity is name+arity: "add/2" and "add/3" are distinct functions.
// Name carries the bare function atom name; Signature carries "name/arity".
// Arity is derived from the named-child count of the expr_args node on the
// function_clause.
//
// Field names (probed):
//
//	fun_decl:             clause  → function_clause
//	function_clause:      name    → atom (function name)
//	                      args    → expr_args (arity = NamedChildCount)
//	                      body    → clause_body
//	module_attribute:     name    → atom
//	behaviour_attribute:  name    → atom
//	record_decl:          name    → atom (record name)
//	import_attribute:     module  → atom; named children include fa nodes
//	pp_define:            (no direct name field; first named child = macro_lhs)
//	macro_lhs:            name    → var (MACRO_NAME)
//
// Call nodes:
//
//	call   — local call: atom + expr_args (e.g. loop(State))
//	remote — remote call: remote_module + call (e.g. math:sqrt(Z))

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	sitter "github.com/malivvan/tree-sitter"

	"github.com/damusix/atomic-claude/atomic/internal/codeintel/extraction"
)

// reLineComment matches an Erlang line comment (% to end of line).
// Used to strip comments before scanning for attributes so that attribute-like
// text inside comments does not produce false matches.
var reLineComment = regexp.MustCompile(`%[^\n]*`)

// reExportAttr matches an Erlang -export([...]) attribute and captures the
// bracketed list contents. It handles multi-line lists by using [\s\S] inside
// the bracket span, stopping at the first closing bracket.
//
// Pattern:  -export([  <contents>  ])
// Capture group 1: everything between the brackets.
var reExportAttr = regexp.MustCompile(`-export\(\[([\s\S]*?)\]\)`)

// reCompileAttr matches a -compile(...) attribute and captures the argument.
// We then check whether the argument contains the atom "export_all".
//
// Pattern:  -compile(  <arg>  )
// Capture group 1: the argument (atom or bracketed list).
var reCompileAttr = regexp.MustCompile(`-compile\(([\s\S]*?)\)`)

// reFA matches a single "name/arity" function/arity token inside an export list,
// e.g. "foo/2" or "bar/0". Only atom characters (lowercase start, alphanumeric +
// underscore) followed by / and one-or-more digits.
var reFA = regexp.MustCompile(`([a-z][a-zA-Z0-9_@]*|\d+)/(\d+)`)

// erlangStripComments removes Erlang line comments (% to end of line) from
// source so that attribute-pattern regexes do not match inside comments.
func erlangStripComments(source string) string {
	return reLineComment.ReplaceAllString(source, "")
}

// erlangExportSet parses all -export([f/N, …]) attributes in source and returns
// a set of "name/arity" strings that are explicitly exported.
// Comments are stripped before scanning so that commented-out export lists are
// not mistakenly included.
func erlangExportSet(source string) map[string]bool {
	stripped := erlangStripComments(source)
	set := make(map[string]bool)
	for _, m := range reExportAttr.FindAllStringSubmatch(stripped, -1) {
		// m[1] is the bracketed list body, e.g. "foo/1, bar/2\n  baz/0"
		for _, fa := range reFA.FindAllStringSubmatch(m[1], -1) {
			set[fa[0]] = true // fa[0] is the full "name/arity" match
		}
	}
	return set
}

// ErlangExtractor returns the LanguageExtractor config for Erlang source files
// (.erl, .hrl).
//
// Node-type strings are verified by parsing representative Erlang source via the
// wazero binding (see tmp/probe-erlang/main.go).
func ErlangExtractor() extraction.LanguageExtractor {
	return extraction.LanguageExtractor{
		// fun_decl covers all top-level function definitions.
		// ResolveBody unwraps fun_decl → function_clause for name/arity extraction.
		FunctionTypes: extraction.TypeSet("fun_decl"),

		// module_attribute: -module(Name). → NodeKindModule.
		ModuleTypes: extraction.TypeSet("module_attribute"),

		// record_decl: -record(Name, {fields}). → NodeKindStruct.
		StructTypes: extraction.TypeSet("record_decl"),

		// record_field: field inside a record declaration → NodeKindField.
		FieldTypes: extraction.TypeSet("record_field"),

		// pp_define: -define(NAME, value). → NodeKindVariable (stored as a named
		// binding; NodeKindConstant is not reachable via VariableTypes but
		// NodeKindVariable is semantically close enough for macro constants).
		// ResolveBody unwraps pp_define → macro_lhs so nameFromNode can extract
		// the macro name via the macro_lhs "name" field.
		VariableTypes: extraction.TypeSet("pp_define"),

		// import_attribute, behaviour_attribute → import edges.
		// ExtractImport returns the module name; export_attribute is intentionally
		// absent — export tracking is handled by IsExported, not ImportTypes.
		ImportTypes: extraction.TypeSet(
			"behaviour_attribute",
			"import_attribute",
		),

		// call: local call — atom + expr_args (e.g. loop(State)).
		// The framework's calleeNameFromNode falls back to the first named child
		// text; for Erlang "call" that is the atom (function name), giving a clean
		// callee name like "add" or "spawn".
		//
		// "remote" is intentionally omitted from CallTypes. A remote call
		// (math:sqrt(Z)) has a "remote" parent with "remote_module" + "call"
		// children. Leaving "remote" unmatched causes visitChildren to descend into
		// it, which then matches the inner "call" node — extracting the callee as
		// "sqrt" (the function name) rather than "math:" (the module prefix). This
		// produces more useful UnresolvedReferences for the resolution layer.
		CallTypes: extraction.TypeSet("call"),

		// NameField is "name" — works for:
		//   function_clause.name → atom (after ResolveBody unwraps fun_decl)
		//   module_attribute.name → atom
		//   record_decl.name → atom
		//   macro_lhs.name → var (after ResolveBody unwraps pp_define → macro_lhs)
		NameField: "name",
		// BodyField is intentionally empty: the extractFunction body scanner uses
		// BodyField to locate a body sub-node on the original (pre-ResolveBody) node.
		// For Erlang, the original node is fun_decl, which has no direct "body" field
		// (the body lives on the inner function_clause, found via ResolveBody → "clause"
		// field → function_clause → "body"). Leaving BodyField empty triggers the
		// fallback path (visitFunctionBody on the full fun_decl node), which DFS-walks
		// into function_clause.clause_body and correctly finds call nodes.
		BodyField: "",

		// ResolveBody: unwraps fun_decl → function_clause, and pp_define → macro_lhs.
		ResolveBody: erlangResolveBody,

		// GetSignature: produces "name/arity" (e.g. "add/2") for function nodes.
		GetSignature: erlangGetSignature,

		// IsExported: true for module_attribute and record_decl (always public);
		// for fun_decl, checks if "name/arity" appears in any export_attribute.
		IsExported: erlangIsExported,

		// ExtractImport: extracts module name from import_attribute and
		// behaviour_attribute nodes.
		ExtractImport: erlangExtractImport,
	}
}

// erlangResolveBody unwraps:
//   - fun_decl → function_clause (via the "clause" field)
//   - pp_define → macro_lhs (first named child)
//
// All other node types are returned unchanged.
func erlangResolveBody(ctx context.Context, node sitter.Node, _ string) (sitter.Node, error) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return node, nil
	}

	switch kind {
	case "fun_decl":
		// fun_decl has a "clause" field pointing to the function_clause.
		child, err := node.ChildByFieldName(ctx, "clause")
		if err != nil {
			return node, nil
		}
		isNull, _ := child.IsNull(ctx)
		if isNull {
			// Fallback: first named child.
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
		// pp_define has no direct "name" field; the name lives inside the first
		// named child: macro_lhs, which has a "name" field → var (MACRO_NAME).
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

// erlangGetSignature produces the Erlang "name/arity" identity string for
// function nodes (fun_decl or function_clause). Returns "" for other node types.
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

	// Get function name from the "name" field (atom).
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

	// Get arity from the "args" field (expr_args) named child count.
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

// erlangHasExportAll reports whether the source contains a -compile(export_all)
// or -compile([…, export_all, …]) directive. When true, all functions are
// exported.
//
// Anchors to the -compile(...) attribute form and strips line comments first so
// that "export_all" appearing only in a comment does not produce a false positive.
func erlangHasExportAll(source string) bool {
	stripped := erlangStripComments(source)
	for _, m := range reCompileAttr.FindAllStringSubmatch(stripped, -1) {
		// m[1] is the argument of -compile(...), e.g. "export_all" or
		// "[debug_info, export_all]". Check for the bare atom inside it.
		if strings.Contains(m[1], "export_all") {
			return true
		}
	}
	return false
}

// erlangIsExported reports whether an Erlang node is part of the module's
// public interface.
//
//   - module_attribute: always true (the module declaration itself is public).
//   - record_decl: always true (records are module-level declarations).
//   - fun_decl: true iff the module declares -compile(export_all) (anchored to
//     the -compile attribute form), OR the function's "name/arity" identity
//     string appears in one of the module's -export([…]) attribute lists.
//
// Export status is determined from the ACTUAL -export([…]) declaration(s), not
// by a bare substring search over the whole source. This prevents false positives
// from "fun name/arity" references, "-spec name/arity" annotations, and comments.
func erlangIsExported(ctx context.Context, node sitter.Node, source string) bool {
	kind, err := node.Kind(ctx)
	if err != nil {
		return false
	}
	switch kind {
	case "module_attribute", "record_decl":
		return true
	case "fun_decl":
		// Short-circuit: -compile(export_all) or -compile([…, export_all, …])
		// makes all functions in the module exported. The check is anchored to the
		// -compile(...) attribute form, not a bare substring.
		if erlangHasExportAll(source) {
			return true
		}
		// Resolve to function_clause to extract name and arity from the AST.
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

		// Look up the identity string in the set of explicitly exported name/arity
		// pairs parsed from the module's -export([…]) attribute(s).
		return erlangExportSet(source)[target]
	}
	return false
}

// erlangExtractImport handles import_attribute and behaviour_attribute nodes,
// returning the referenced module name as both the import name and path.
// export_attribute is intentionally absent from ImportTypes — export tracking
// is handled by IsExported.
func erlangExtractImport(ctx context.Context, node sitter.Node, source string) (name string, path string) {
	kind, err := node.Kind(ctx)
	if err != nil {
		return "", ""
	}

	switch kind {
	case "behaviour_attribute":
		// -behaviour(Mod). → import edge to Mod.
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
		// -import(Mod, [f/N, …]). → module name as import path.
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
