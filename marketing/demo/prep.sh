#!/usr/bin/env bash
# One-time preparation of the realm the demo records against. Idempotent.
#
#   marketing/demo/prep.sh [<realm-root>]      default: $DEMO_ROOT, else ~/projects/noorm
#
# What it does, and why each step is here rather than in run.mjs:
#   1. Builds bin/atomic from this checkout — the tour must show the code on
#      the branch you are on, not whatever `atomic` is on PATH.
#   2. Indexes every member (`atomic code index` fans out from a realm root)
#      so the schema and code-graph scenes have data. Incremental; cheap on
#      re-run.
#   3. Seeds one scratchpad bundle on the plan the Plans scene opens, so the
#      scratchpad column has something to show. Gitignored, inside the member.
#   4. Stops any running bus daemon so the chat scene starts with an empty
#      roster instead of members left over from earlier sessions.
#
# Not done here: refreshing the wiki. That is /refresh-wiki in a Claude
# session at the realm root — run it first if the wiki pages look stale.

set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/../.." && pwd)"
ROOT="${1:-${DEMO_ROOT:-$HOME/projects/noorm}}"
PLAN_MEMBER="${DEMO_PLAN_MEMBER:-monorepo}"
PLAN_SLUG="${DEMO_PLAN_SLUG:-config-access-roles}"

echo "== build"
make -C "$REPO/atomic" build >/dev/null
BIN="$REPO/bin/atomic"

echo "== index $ROOT"
(cd "$ROOT" && "$BIN" code index)

echo "== seed bundle $PLAN_MEMBER/$PLAN_SLUG"
if [ ! -f "$ROOT/$PLAN_MEMBER/.claude/.scratchpad/$PLAN_SLUG/meta.toml" ]; then
  (cd "$ROOT/$PLAN_MEMBER" && "$BIN" scratchpad new "$PLAN_SLUG" --purpose implement)
else
  echo "already present"
fi

echo "== bus daemon"
"$BIN" bus stop || true

echo
echo "ready: node $HERE/run.mjs --root $ROOT"
