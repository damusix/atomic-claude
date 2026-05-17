#!/usr/bin/env bash
set -euo pipefail

REPO="$(cd "$(dirname "$0")/.." && pwd)"
DEST="$REPO/.claude"

mkdir -p "$DEST"/{agents,commands,output-styles,skills,rules}

for type in agents commands output-styles; do
    [ -d "$REPO/$type" ] || continue
    for f in "$REPO/$type"/*.md; do
        [ -e "$f" ] || continue
        ln -sfn "$f" "$DEST/$type/$(basename "$f")"
    done
done

for parent in skills rules; do
    [ -d "$REPO/$parent" ] || continue
    for dir in "$REPO/$parent"/*/; do
        [ -d "$dir" ] || continue
        name="$(basename "$dir")"
        mkdir -p "$DEST/$parent/$name"
        for f in "$dir"*; do
            [ -e "$f" ] || continue
            ln -sfn "$f" "$DEST/$parent/$name/$(basename "$f")"
        done
    done
done
