package sitter

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

const Version = "v0.24.7"

//go:embed lib/ts.wasm
var tsWasm []byte

type TreeSitter struct {
	m api.Module

	malloc api.Function
	free   api.Function
	strlen api.Function

	parserNew         api.Function
	parserParseString api.Function
	parserDelete      api.Function
	parserSetLanguage api.Function

	languageVersion api.Function

	treeRootNode api.Function

	queryNew              api.Function
	queryCursorNew        api.Function
	queryCusorExec        api.Function
	queryCursorNextMatch  api.Function
	queryCaptureNameForID api.Function

	nodeString           api.Function
	nodeChildCount       api.Function
	nodeNamedChildCount  api.Function
	nodeChild            api.Function
	nodeNamedChild       api.Function
	nodeType             api.Function
	nodeEndByte          api.Function
	nodeStartByte        api.Function
	nodeIsError          api.Function
	nodeIsNull           api.Function
	nodeChildByFieldName api.Function
	nodePrevNamedSibling api.Function

	// Grammar language functions (21 languages)
	languageC          api.Function
	languageCpp        api.Function
	languageCSharp     api.Function
	languageJava       api.Function
	languageJavaScript api.Function
	languageGo         api.Function
	languageKotlin     api.Function
	languageLua        api.Function
	languagePHP        api.Function
	languagePython     api.Function
	languageRuby       api.Function
	languageRust       api.Function
	languageScala      api.Function
	languageSwift      api.Function
	languageTypescript api.Function
	languageTSX        api.Function
	languageDart       api.Function
	languageLuau       api.Function
	languageObjC       api.Function
	languagePascal     api.Function
	languageElixir     api.Function
}

func New(ctx context.Context) (TreeSitter, error) {
	r := wazero.NewRuntime(ctx)

	wasi_snapshot_preview1.MustInstantiate(ctx, r)

	compiled, err := r.CompileModule(ctx, tsWasm)
	if err != nil {
		return TreeSitter{}, fmt.Errorf("compiling wasm module: %w", err)
	}

	mod, err := r.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
	if err != nil {
		return TreeSitter{}, fmt.Errorf("instantiating module: %w", err)
	}

	return TreeSitter{
		m:                     mod,
		malloc:                mod.ExportedFunction("malloc"),
		free:                  mod.ExportedFunction("free"),
		strlen:                mod.ExportedFunction("strlen"),
		parserNew:             mod.ExportedFunction("ts_parser_new"),
		parserParseString:     mod.ExportedFunction("ts_parser_parse_string"),
		parserSetLanguage:     mod.ExportedFunction("ts_parser_set_language"),
		parserDelete:          mod.ExportedFunction("ts_parser_delete"),
		queryNew:              mod.ExportedFunction("ts_query_new"),
		queryCursorNew:        mod.ExportedFunction("ts_query_cursor_new"),
		queryCusorExec:        mod.ExportedFunction("ts_query_cursor_exec"),
		queryCursorNextMatch:  mod.ExportedFunction("ts_query_cursor_next_match"),
		queryCaptureNameForID: mod.ExportedFunction("ts_query_capture_name_for_id"),
		languageVersion:       mod.ExportedFunction("ts_language_version"),
		treeRootNode:          mod.ExportedFunction("ts_tree_root_node"),
		nodeString:            mod.ExportedFunction("ts_node_string"),
		nodeChildCount:        mod.ExportedFunction("ts_node_child_count"),
		nodeNamedChildCount:   mod.ExportedFunction("ts_node_named_child_count"),
		nodeChild:             mod.ExportedFunction("ts_node_child"),
		nodeNamedChild:        mod.ExportedFunction("ts_node_named_child"),
		nodeType:              mod.ExportedFunction("ts_node_type"),
		nodeStartByte:         mod.ExportedFunction("ts_node_start_byte"),
		nodeEndByte:           mod.ExportedFunction("ts_node_end_byte"),
		nodeIsError:           mod.ExportedFunction("ts_node_is_error"),
		nodeIsNull:            mod.ExportedFunction("ts_node_is_null"),
		nodeChildByFieldName:  mod.ExportedFunction("ts_node_child_by_field_name"),
		nodePrevNamedSibling:  mod.ExportedFunction("ts_node_prev_named_sibling"),
		// Grammar language functions
		languageC:          mod.ExportedFunction("tree_sitter_c"),
		languageCpp:        mod.ExportedFunction("tree_sitter_cpp"),
		languageCSharp:     mod.ExportedFunction("tree_sitter_c_sharp"),
		languageJava:       mod.ExportedFunction("tree_sitter_java"),
		languageJavaScript: mod.ExportedFunction("tree_sitter_javascript"),
		languageGo:         mod.ExportedFunction("tree_sitter_go"),
		languageKotlin:     mod.ExportedFunction("tree_sitter_kotlin"),
		languageLua:        mod.ExportedFunction("tree_sitter_lua"),
		languagePHP:        mod.ExportedFunction("tree_sitter_php"),
		languagePython:     mod.ExportedFunction("tree_sitter_python"),
		languageRuby:       mod.ExportedFunction("tree_sitter_ruby"),
		languageRust:       mod.ExportedFunction("tree_sitter_rust"),
		languageScala:      mod.ExportedFunction("tree_sitter_scala"),
		languageSwift:      mod.ExportedFunction("tree_sitter_swift"),
		languageTypescript: mod.ExportedFunction("tree_sitter_typescript"),
		languageTSX:        mod.ExportedFunction("tree_sitter_tsx"),
		languageDart:       mod.ExportedFunction("tree_sitter_dart"),
		languageLuau:       mod.ExportedFunction("tree_sitter_luau"),
		languageObjC:       mod.ExportedFunction("tree_sitter_objc"),
		languagePascal:     mod.ExportedFunction("tree_sitter_pascal"),
		languageElixir:     mod.ExportedFunction("tree_sitter_elixir"),
	}, nil
}

func (t TreeSitter) allocateString(
	ctx context.Context,
	str string,
) (ptr uint64, size uint64, free func(), err error) {
	strByte := []byte(str)
	strSize := uint64(len(strByte))
	strPtr, err := t.malloc.Call(ctx, strSize)
	if err != nil {
		return 0, 0, nil, fmt.Errorf("allocating string: %w", err)
	}

	if !t.m.Memory().Write(uint32(strPtr[0]), strByte) {
		return 0, 0, nil, fmt.Errorf("writing string to wasm memory: out of bounds at ptr=%d len=%d", strPtr[0], strSize)
	}

	return strPtr[0], strSize, func() {
		t.free.Call(context.Background(), strPtr[0])
	}, nil
}

func (t TreeSitter) readString(ctx context.Context, ptr uint64) (string, error) {
	strSize, err := t.strlen.Call(ctx, ptr)
	if err != nil {
		return "", fmt.Errorf("getting string length: %w", err)
	}
	strBytes, ok := t.m.Memory().Read(uint32(ptr), uint32(strSize[0]))
	if !ok {
		return "", errors.New("error reading string")
	}
	return string(strBytes), nil
}
