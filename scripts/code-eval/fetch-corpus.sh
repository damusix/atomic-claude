#!/usr/bin/env bash
# Fetch (shallow-clone or update) the code-intel evaluation corpus.
#
# Reads scripts/code-eval/corpus.tsv and clones each repo into
# tmp/code-eval/repos/<id> at the pinned ref (shallow, --depth 1). Idempotent:
# an existing repo is fetched + re-checked-out at the ref. Failures are skipped
# and reported, never fatal — a missing/renamed repo does not abort the run.
#
# Usage:
#   scripts/code-eval/fetch-corpus.sh [id...]    # all, or only the given ids
#
# Records the resolved commit SHA per repo into tmp/code-eval/repos/<id>.sha so
# eval output is attributable to an exact commit (reproducibility for goldens).

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
MANIFEST="$SCRIPT_DIR/corpus.tsv"
DEST="$REPO_ROOT/tmp/code-eval/repos"

mkdir -p "$DEST"

want=("$@")  # optional id filter

wanted() {
  [ ${#want[@]} -eq 0 ] && return 0
  local id="$1"; local w
  for w in "${want[@]}"; do [ "$w" = "$id" ] && return 0; done
  return 1
}

ok=0; fail=0; skip=0
printf '%-18s %-10s %-9s %s\n' "ID" "STATUS" "REF" "RESOLVED"
printf '%s\n' "------------------------------------------------------------------------"

while IFS=$'\t' read -r id kind language url ref note; do
  case "$id" in ''|\#*) continue;; esac
  wanted "$id" || { skip=$((skip+1)); continue; }
  dir="$DEST/$id"

  if [ -d "$dir/.git" ]; then
    # update existing shallow clone to the ref
    git -C "$dir" fetch --depth 1 origin "$ref" >/dev/null 2>&1 \
      && git -C "$dir" checkout -q FETCH_HEAD >/dev/null 2>&1
    rc=$?
  else
    rm -rf "$dir"
    # try the pinned ref first; on failure fall back to the default branch
    if git clone --depth 1 --branch "$ref" "$url" "$dir" >/dev/null 2>&1; then
      rc=0
    elif git clone --depth 1 "$url" "$dir" >/dev/null 2>&1; then
      rc=0; ref="(default)"
    else
      rc=1
    fi
  fi

  if [ $rc -eq 0 ] && [ -d "$dir/.git" ]; then
    sha="$(git -C "$dir" rev-parse --short HEAD 2>/dev/null)"
    echo "$sha" > "$DEST/$id.sha"
    printf '%-18s %-10s %-9s %s\n' "$id" "ok" "$ref" "$sha"
    ok=$((ok+1))
  else
    printf '%-18s %-10s %-9s %s\n' "$id" "FAILED" "$ref" "(clone failed)"
    fail=$((fail+1))
  fi
done < "$MANIFEST"

printf '%s\n' "------------------------------------------------------------------------"
echo "fetched: $ok  failed: $fail  skipped: $skip"
echo "corpus at: $DEST"
[ $fail -eq 0 ]
