.PHONY: build clean
default: build

# Build the 19-grammar ts.wasm for the atomic code-intel engine.
#
# Regen command (run from this directory):
#   make build
#
# Prerequisites: zig >= 0.13.0 on PATH (build-time only; the shipped binary is CGO_ENABLED=0/wazero).
#
# Grammar versions pinned in src/_gen/grammars.json (malivvan upstream grammars) and:
#   dart:   https://github.com/UserNobody14/tree-sitter-dart  @ 311a009 (ABI 14; a9bdfa3 bumped to ABI 15)
#   luau:   https://github.com/amaanq/tree-sitter-luau        @ a8914d6c1fc5131f8e1c13f769fa704c9f5eb02f
#   objc:   https://github.com/amaanq/tree-sitter-objc        @ 181a81b8f23a2d593e7ab4259981f50122909fda
#   pascal: https://github.com/Isopod/tree-sitter-pascal      @ 042119eca2e18a60e56317fb06ee3ba5c32cb447
#
# Malivvan upstream grammars sourced from:
#   https://github.com/malivvan/tree-sitter  (src/ directory)

build:
	@mkdir -p lib
	@rm -f lib/ts.wasm
	@zig cc --target=wasm32-wasi-musl -mexec-model=reactor \
		-I src/ \
		src/lib.c \
		src/c/parser.c \
		src/cpp/parser.c src/cpp/scanner.c \
		src/csharp/parser.c src/csharp/scanner.c \
		src/java/parser.c \
		src/javascript/parser.c src/javascript/scanner.c \
		src/golang/parser.c \
		src/kotlin/parser.c src/kotlin/scanner.c \
		src/lua/parser.c src/lua/scanner.c \
		src/php/parser.c src/php/scanner.c \
		src/python/parser.c src/python/scanner.c \
		src/ruby/parser.c src/ruby/scanner.c \
		src/rust/parser.c src/rust/scanner.c \
		src/scala/parser.c src/scala/scanner.c \
		src/swift/parser.c src/swift/scanner.c \
		src/typescript/typescript/parser.c src/typescript/typescript/scanner.c \
		src/typescript/tsx/parser.c src/typescript/tsx/scanner.c \
		src/dart/parser.c src/dart/scanner.c \
		src/luau/parser.c src/luau/scanner.c \
		src/objc/parser.c \
		src/pascal/parser.c \
		-o lib/ts.wasm \
		-Os -fPIC \
		-Wl,--no-entry \
		-Wl,-z -Wl,stack-size=65536 \
		-Wl,--strip-debug \
		-Wl,--import-symbols \
		-Wl,--export=malloc \
		-Wl,--export=free \
		-Wl,--export=strlen \
		-Wl,--export=ts_parser_new \
		-Wl,--export=ts_parser_parse_string \
		-Wl,--export=ts_parser_set_language \
		-Wl,--export=ts_parser_delete \
		-Wl,--export=ts_query_new \
		-Wl,--export=ts_query_cursor_new \
		-Wl,--export=ts_query_cursor_exec \
		-Wl,--export=ts_query_cursor_next_match \
		-Wl,--export=ts_query_capture_name_for_id \
		-Wl,--export=ts_language_name \
		-Wl,--export=ts_language_version \
		-Wl,--export=ts_tree_root_node \
		-Wl,--export=ts_node_string \
		-Wl,--export=ts_node_child_count \
		-Wl,--export=ts_node_named_child_count \
		-Wl,--export=ts_node_child \
		-Wl,--export=ts_node_named_child \
		-Wl,--export=ts_node_type \
		-Wl,--export=ts_node_start_byte \
		-Wl,--export=ts_node_end_byte \
		-Wl,--export=ts_node_is_error \
		-Wl,--export=ts_node_is_null \
		-Wl,--export=ts_node_child_by_field_name \
		-Wl,--export=ts_node_prev_named_sibling \
		-Wl,--export=tree_sitter_c \
		-Wl,--export=tree_sitter_cpp \
		-Wl,--export=tree_sitter_c_sharp \
		-Wl,--export=tree_sitter_java \
		-Wl,--export=tree_sitter_javascript \
		-Wl,--export=tree_sitter_go \
		-Wl,--export=tree_sitter_kotlin \
		-Wl,--export=tree_sitter_lua \
		-Wl,--export=tree_sitter_php \
		-Wl,--export=tree_sitter_python \
		-Wl,--export=tree_sitter_ruby \
		-Wl,--export=tree_sitter_rust \
		-Wl,--export=tree_sitter_scala \
		-Wl,--export=tree_sitter_swift \
		-Wl,--export=tree_sitter_typescript \
		-Wl,--export=tree_sitter_tsx \
		-Wl,--export=tree_sitter_dart \
		-Wl,--export=tree_sitter_luau \
		-Wl,--export=tree_sitter_objc \
		-Wl,--export=tree_sitter_pascal
	@echo "Built lib/ts.wasm: $$(du -h lib/ts.wasm | cut -f1)"

clean:
	rm -f lib/ts.wasm
