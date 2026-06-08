# Code-intelligence engine evaluation harness

Real-repo end-to-end tests for the `atomic code` engine. Indexes a corpus of real
projects (one+ per language, plus real apps using each detected framework) and runs
a battery of challenging `atomic code` commands, capturing structural metrics +
indexing throughput. Used both as a manual eval and as a regression smoke baseline.

## Layout

- `corpus.tsv` — the repo manifest (committed). Tab-separated; pinned refs.
- `fetch-corpus.sh [id...]` — shallow-clone/update the corpus into `tmp/code-eval/repos/` (gitignored).
- `run-eval.sh [id...]` — index each repo (timed, capped) + run the command battery →
  `tmp/code-eval/out/<id>/{metrics.json,commands.txt,index.txt,status.json}` + `out/SUMMARY.md`.
- `golden/` — committed structural goldens (counts/kinds/routes per pinned repo), once validated.

## Usage

```bash
scripts/code-eval/fetch-corpus.sh              # clone all (resilient; skips failures)
scripts/code-eval/run-eval.sh                  # index + battery over all fetched
scripts/code-eval/run-eval.sh rw-gin rw-express   # only specific repos
INDEX_TIMEOUT=300 scripts/code-eval/run-eval.sh   # raise the per-repo index cap (default 180s)
```

## What it measures

- **Indexing throughput** (`index_seconds`, `files/s`, `timed_out`) — first-class; a perf
  cliff shows as a low files/s or `timed_out=yes`.
- **Extraction** (files/nodes/edges + `nodesByKind`) — proves the language extractors work.
- **Framework route detection** (`routes` count) — only the `kind:framework` rows (real apps)
  define routes; lib repos won't show routes.
- **Query surface** (`commands.txt`) — search/callers/callees/impact/node/explore/affected on
  real symbols, for eyeball + crash-resistance.

The corpus is pinned (tags) so structural metrics are reproducible and can graduate to
committed goldens for CI regression checks.
