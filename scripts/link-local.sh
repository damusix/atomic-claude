#!/usr/bin/env bash
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
# Artifacts are sourced from context/ (the tree that installs to ~/.claude) and
# linked into the repo's own .claude/ so this repo dogfoods its own config.
SRC="$REPO/context"
DEST="$REPO/.claude"

mkdir -p "$DEST"/{agents,commands,output-styles,skills,rules}

for type in agents commands output-styles; do
    [ -d "$SRC/$type" ] || continue
    for f in "$SRC/$type"/*.md; do
        [ -e "$f" ] || continue
        ln -sfn "$f" "$DEST/$type/$(basename "$f")"
    done
done

for parent in skills rules; do
    [ -d "$SRC/$parent" ] || continue
    for dir in "$SRC/$parent"/*/; do
        [ -d "$dir" ] || continue
        name="$(basename "$dir")"
        mkdir -p "$DEST/$parent/$name"
        for f in "$dir"*; do
            [ -e "$f" ] || continue
            ln -sfn "$f" "$DEST/$parent/$name/$(basename "$f")"
        done
    done
done
