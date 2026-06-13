#!/usr/bin/env bash
#
# Fetch tree-sitter grammar sources into ./src from the pins in grammars.json.
#
# Maintainer-only, cold path. The shipped binary depends on lib/ts.wasm (committed);
# ./src is build-time input only, fetched on demand and gitignored. Run this (or
# `make fetch`) only when rebuilding the wasm — see CLAUDE.md.
#
# Requires: git, jq.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
manifest="$here/grammars.json"
src="$here/src"

for tool in git jq; do
  command -v "$tool" >/dev/null 2>&1 || { echo "fetch-grammars: '$tool' is required" >&2; exit 1; }
done
[ -f "$manifest" ] || { echo "fetch-grammars: $manifest not found" >&2; exit 1; }

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

clone_at() { # url revision dest
  git clone --quiet "$1" "$2"
  git -C "$2" checkout --quiet "$3"
}

echo "fetch-grammars: resetting $src"
rm -rf "$src"
mkdir -p "$src"

# --- runtime + bundled grammars (single upstream) ---
rt_url="$(jq -r '.runtime.url' "$manifest")"
rt_rev="$(jq -r '.runtime.revision' "$manifest")"
echo "fetch-grammars: runtime $rt_url @ ${rt_rev:0:12}"
clone_at "$rt_url" "$work/runtime" "$rt_rev"
# runtime C/headers live at the src root
cp "$work"/runtime/src/*.c "$work"/runtime/src/*.h "$src"/
# each bundled grammar is a whole subdir (carries its own parser.h + scanner.c when present)
while IFS= read -r g; do
  [ -d "$work/runtime/src/$g" ] || { echo "fetch-grammars: grammar dir '$g' missing in runtime upstream" >&2; exit 1; }
  cp -R "$work/runtime/src/$g" "$src/$g"
done < <(jq -r '.runtime.grammars[]' "$manifest")

# --- external grammars (one upstream each) ---
hdr_from="$(jq -r '.grammar_header_from' "$manifest")"
count="$(jq '.external | length' "$manifest")"
for i in $(seq 0 $((count - 1))); do
  lang="$(jq -r ".external[$i].language" "$manifest")"
  url="$(jq -r ".external[$i].url" "$manifest")"
  rev="$(jq -r ".external[$i].revision" "$manifest")"
  echo "fetch-grammars: external $lang $url @ ${rev:0:12}"
  clone_at "$url" "$work/$lang" "$rev"
  mkdir -p "$src/$lang"
  while IFS= read -r f; do
    cp "$work/$lang/src/$f" "$src/$lang/$f"
  done < <(jq -r ".external[$i].files[]" "$manifest")
  # grammar-facing header: external grammars include "tree_sitter/parser.h"
  if [ "$lang" = "$hdr_from" ]; then
    mkdir -p "$src/tree_sitter"
    cp "$work/$lang/src/tree_sitter/parser.h" "$src/tree_sitter/parser.h"
  fi
done

echo "fetch-grammars: done -> $src"
