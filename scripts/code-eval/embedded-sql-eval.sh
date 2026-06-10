#!/usr/bin/env bash
# embedded-sql-eval.sh — CP5: Corpus harness for the embedded-SQL post-pass.
#
# Indexes a real-repo corpus with the embedded SQL post-pass and validates:
#   1. Referential integrity: every edge with provenance='embedded' has both
#      source and target in the nodes table.  FAIL = hard exit 1.
#   2. Admission surface: count + dump all string literals that passed the
#      IsSQLLiteral gate (FP candidates for human eyeball).
#
# Primary corpus: a fetched real repo from tmp/code-eval/repos/ (any Go, Python,
# or TypeScript repo).  Guaranteed fallback: this repo's own atomic/ Go tree
# (adversarial FP corpus — contains SQL-keyword string literals as regex
# patterns and comment text; a strict gate must reject them).
#
# Harness RUNS HEADLESS without network: the fallback corpus is local.
#
# Usage:
#   scripts/code-eval/embedded-sql-eval.sh [corpus-id]
#
#   corpus-id (optional): id from corpus.tsv to use as primary corpus.
#   If the id is not fetched, the harness silently falls back to local corpus.
#
# Output: tmp/code-eval/out/embedded-sql/<corpus-id>/admission.txt
#         tmp/code-eval/out/embedded-sql/<corpus-id>/summary.txt
#         prints key lines to stdout.
#
# Exit codes:
#   0: referential integrity PASS
#   1: referential integrity FAIL or fatal error

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
REPOS="$REPO_ROOT/tmp/code-eval/repos"

# ---- binaries ----
ATOMIC_BIN="$REPO_ROOT/bin/atomic"
ADMISSION_BIN="$REPO_ROOT/bin/embedded-sql-admission"

# Build atomic binary if missing.
if [ ! -x "$ATOMIC_BIN" ]; then
  echo "building bin/atomic…"
  ( cd "$REPO_ROOT/atomic" && CGO_ENABLED=0 go build -o ../bin/atomic ./cmd/atomic ) || {
    echo "FATAL: failed to build bin/atomic"; exit 1; }
fi

# Build admission tool if missing.
if [ ! -x "$ADMISSION_BIN" ]; then
  echo "building bin/embedded-sql-admission…"
  ( cd "$REPO_ROOT/atomic" && CGO_ENABLED=0 go build -o ../bin/embedded-sql-admission ./cmd/embedded-sql-admission ) || {
    echo "FATAL: failed to build bin/embedded-sql-admission"; exit 1; }
fi

# ---- corpus selection ----
CORPUS_ID="${1:-}"
CORPUS_DIR=""
CORPUS_LABEL=""

# Built-in corpus: "multilang" resolves to the committed multilang fixture dir.
# This corpus spans Ruby, Java, PHP, Rust, Kotlin, and Lua source files and
# exercises the CP4 zero-phantom-edge bar across the new host languages.
# It is NOT a git repo; the indexer falls back to WalkDir automatically.
if [ "$CORPUS_ID" = "multilang" ]; then
  CORPUS_DIR="$SCRIPT_DIR/fixtures/embedded-sql-multilang"
  CORPUS_LABEL="multilang (built-in fixture corpus)"
  if [ ! -d "$CORPUS_DIR" ]; then
    echo "FATAL: built-in multilang corpus not found at $CORPUS_DIR"
    exit 1
  fi
fi

# Try the requested corpus ID as a fetched repo next.
if [ -z "$CORPUS_DIR" ] && [ -n "$CORPUS_ID" ] && [ -d "$REPOS/$CORPUS_ID/.git" ]; then
  CORPUS_DIR="$REPOS/$CORPUS_ID"
  CORPUS_LABEL="$CORPUS_ID (fetched)"
fi

# Fallback to local atomic/ Go tree (guaranteed headless).
if [ -z "$CORPUS_DIR" ]; then
  if [ -n "$CORPUS_ID" ]; then
    echo "note: corpus '$CORPUS_ID' not fetched; falling back to local atomic/ tree"
  fi
  CORPUS_DIR="$REPO_ROOT/atomic"
  CORPUS_LABEL="atomic/ (local fallback)"
fi

OUT="$REPO_ROOT/tmp/code-eval/out/embedded-sql/${CORPUS_LABEL// /_}"
mkdir -p "$OUT"

echo "========================================"
echo "  Embedded-SQL corpus eval"
echo "  corpus: $CORPUS_LABEL"
echo "  dir:    $CORPUS_DIR"
echo "  out:    $OUT"
echo "========================================"
echo

# ---- step 1: fresh index ----
echo "--- step 1: index corpus ---"
rm -rf "$CORPUS_DIR/.claude/.atomic-index"
"$ATOMIC_BIN" --repo "$CORPUS_DIR" code index 2>&1 | tail -5
ISTAT=$?
if [ $ISTAT -ne 0 ]; then
  echo "FATAL: index failed (exit $ISTAT)"
  exit 1
fi

DB="$CORPUS_DIR/.claude/.atomic-index/atomic.db"
if [ ! -f "$DB" ]; then
  echo "FATAL: DB not found at $DB after index"
  exit 1
fi
echo "DB: $DB"
echo

# ---- step 2: referential integrity check ----
echo "--- step 2: referential integrity (embedded edges) ---"

# Sanity guard: require a non-empty index before any verdict is meaningful.
NODE_COUNT=$(sqlite3 "$DB" \
  "SELECT COUNT(*) FROM nodes" 2>/dev/null || echo 0)
echo "  total nodes in index: $NODE_COUNT"

# Count embedded edges.
EMBEDDED_COUNT=$(sqlite3 "$DB" \
  "SELECT COUNT(*) FROM edges WHERE provenance = 'embedded'" 2>/dev/null || echo 0)
echo "  embedded edges total: $EMBEDDED_COUNT"

# Find edges where source is not in nodes.
DANGLING_SOURCE=$(sqlite3 "$DB" \
  "SELECT COUNT(*) FROM edges e WHERE e.provenance = 'embedded'
     AND NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = e.source)" 2>/dev/null || echo "ERROR")

# Find edges where target is not in nodes (covers unresolved refs stored as edges,
# which may legitimately have targets not in nodes — these are from the resolution
# pipeline and only become resolved edges post-resolution; pre-resolution they
# are UnresolvedReferences and never written to edges at all).
# For embedded edges that ARE in the edges table, they have been through
# pipeline.createEdges and the target should be a real node ID or absent
# (unresolved refs become edges only when resolved). Check both.
DANGLING_TARGET=$(sqlite3 "$DB" \
  "SELECT COUNT(*) FROM edges e WHERE e.provenance = 'embedded'
     AND NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = e.target)" 2>/dev/null || echo "ERROR")

echo "  dangling source (source not in nodes): $DANGLING_SOURCE"
echo "  dangling target (target not in nodes): $DANGLING_TARGET"
echo

INTEGRITY_PASS=true
if [ "$DANGLING_SOURCE" = "ERROR" ] || [ "$DANGLING_TARGET" = "ERROR" ]; then
  INTEGRITY_PASS=false
  echo "REFERENTIAL INTEGRITY: ERROR (sqlite3 query failed)"
else
  if [ "$DANGLING_SOURCE" -ne 0 ]; then
    INTEGRITY_PASS=false
    echo "REFERENTIAL INTEGRITY: FAIL — $DANGLING_SOURCE edges with dangling source"
    echo "  dumping dangling source edges:"
    sqlite3 "$DB" \
      "SELECT e.id, e.source, e.target, e.kind FROM edges e
         WHERE e.provenance = 'embedded'
           AND NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = e.source)
         LIMIT 20" 2>/dev/null
  fi
  if [ "$DANGLING_TARGET" -ne 0 ]; then
    INTEGRITY_PASS=false
    echo "REFERENTIAL INTEGRITY: FAIL — $DANGLING_TARGET edges with dangling target"
    echo "  dumping dangling target edges:"
    sqlite3 "$DB" \
      "SELECT e.id, e.source, e.target, e.kind FROM edges e
         WHERE e.provenance = 'embedded'
           AND NOT EXISTS (SELECT 1 FROM nodes n WHERE n.id = e.target)
         LIMIT 20" 2>/dev/null
  fi
  if [ "$DANGLING_SOURCE" -eq 0 ] && [ "$DANGLING_TARGET" -eq 0 ]; then
    # Guard 1: empty index → vacuous PASS is meaningless; the pipeline may be broken.
    if [ "$NODE_COUNT" -eq 0 ]; then
      INTEGRITY_PASS=false
      echo "REFERENTIAL INTEGRITY: FAIL — index is empty (0 nodes); indexer produced no output"
      echo "  This is a pipeline failure, not a clean corpus. Re-run after fixing the indexer."
    # Guard 2: no embedded edges on a known SQL corpus → post-pass is not firing.
    elif [ "$EMBEDDED_COUNT" -eq 0 ]; then
      INTEGRITY_PASS=false
      echo "REFERENTIAL INTEGRITY: FAIL — 0 embedded edges on a non-empty index ($NODE_COUNT nodes)"
      echo "  The embedded-SQL post-pass produced no edges. Expected >= 1 on the atomic/ corpus."
    else
      echo "REFERENTIAL INTEGRITY: PASS ($EMBEDDED_COUNT edges, zero dangling, $NODE_COUNT nodes)"
    fi
  fi
fi

# Show a sample of embedded edges for inspection.
if [ "$EMBEDDED_COUNT" -gt 0 ]; then
  echo
  echo "  sample embedded edges (up to 10):"
  sqlite3 "$DB" \
    "SELECT e.source, e.target, e.kind, e.line FROM edges e
       WHERE e.provenance = 'embedded' LIMIT 10" 2>/dev/null \
    | sed 's/^/    /'
fi
echo

# ---- step 3: admission surface ----
echo "--- step 3: admission surface (IsSQLLiteral gate) ---"
ADMIT_OUT="$OUT/admission.txt"
"$ADMISSION_BIN" "$CORPUS_DIR" > "$ADMIT_OUT" 2>&1
ADMIT_RC=$?
if [ $ADMIT_RC -ne 0 ]; then
  echo "  admission tool failed (exit $ADMIT_RC)"
  cat "$ADMIT_OUT"
else
  # Extract key stats.
  SCANNED=$(grep "^TOTAL_LITERALS_SCANNED:" "$ADMIT_OUT" | awk '{print $2}')
  ADMITTED=$(grep "^ADMITTED_COUNT:" "$ADMIT_OUT" | awk '{print $2}')
  echo "  literals scanned: ${SCANNED:-?}"
  echo "  literals admitted (IsSQLLiteral=true): ${ADMITTED:-?}"
  echo "  full admission dump: $ADMIT_OUT"
  echo
  if grep -q "^---ADMITTED LITERALS---" "$ADMIT_OUT"; then
    echo "  admitted literals:"
    grep -A9999 "^---ADMITTED LITERALS---" "$ADMIT_OUT" | tail -n +2 | head -30 | sed 's/^/    /'
    ADMIT_LINES=$(grep -A9999 "^---ADMITTED LITERALS---" "$ADMIT_OUT" | tail -n +2 | wc -l | tr -d ' ')
    if [ "${ADMIT_LINES:-0}" -gt 30 ]; then
      echo "    ... (${ADMIT_LINES} total; see $ADMIT_OUT)"
    fi
  fi
fi
echo

# ---- summary ----
echo "========================================"
echo "  SUMMARY"
echo "  corpus:             $CORPUS_LABEL"
echo "  total nodes:        $NODE_COUNT"
echo "  embedded edges:     $EMBEDDED_COUNT"
echo "  dangling source:    $DANGLING_SOURCE"
echo "  dangling target:    $DANGLING_TARGET"
echo "  literals scanned:   ${SCANNED:-?}"
echo "  literals admitted:  ${ADMITTED:-?}"
if $INTEGRITY_PASS; then
  echo "  REFERENTIAL INTEGRITY: PASS"
else
  echo "  REFERENTIAL INTEGRITY: FAIL"
fi
echo "========================================"

# Write a machine-readable summary for CI/orchestrator consumption.
{
  echo "corpus=$CORPUS_LABEL"
  echo "total_nodes=$NODE_COUNT"
  echo "embedded_edges=$EMBEDDED_COUNT"
  echo "dangling_source=$DANGLING_SOURCE"
  echo "dangling_target=$DANGLING_TARGET"
  echo "literals_scanned=${SCANNED:-?}"
  echo "literals_admitted=${ADMITTED:-?}"
  echo "integrity_pass=$INTEGRITY_PASS"
} > "$OUT/summary.txt"
cat "$OUT/summary.txt"

if ! $INTEGRITY_PASS; then
  exit 1
fi
exit 0
