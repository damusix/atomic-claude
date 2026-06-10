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

## Embedded-SQL gate eval (`embedded-sql-eval.sh`)

Validates the embedded SQL post-pass (CP2-CP4) against a real corpus:

```bash
scripts/code-eval/embedded-sql-eval.sh              # local fallback corpus (headless)
scripts/code-eval/embedded-sql-eval.sh multilang    # built-in multilang fixture corpus
scripts/code-eval/embedded-sql-eval.sh rw-gin       # real fetched repo (fetch first)
scripts/code-eval/embedded-sql-eval.sh gin-lib      # any corpus.tsv id
```

What it checks:

- **Referential integrity** (hard assert): every edge with `provenance='embedded'`
  in the SQLite index has its `source` and `target` present in the `nodes` table.
  Any dangling edge causes exit 1.
- **Admission surface**: count + dump of string literals that passed the
  `IsSQLLiteral` gate — FP candidates for human eyeball.

### Corpora

| Corpus id | Location | Languages | Notes |
|-----------|----------|-----------|-------|
| *(none)* | `atomic/` Go tree | Go | Adversarial FP corpus — SQL-keyword strings as regex patterns/comments. Guaranteed local, headless. |
| `multilang` | `scripts/code-eval/fixtures/embedded-sql-multilang/` | Ruby, Java, PHP, Rust, Kotlin, Lua | CP4 built-in corpus. Committed fixtures covering new host languages. Does not require a git repo (WalkDir fallback). |
| *any id* | `tmp/code-eval/repos/<id>/` | varies | Fetched via `fetch-corpus.sh`. Requires prior fetch. Falls back to local if not present. |

Primary corpus: a fetched real repo from `tmp/code-eval/repos/`.
Built-in corpus: `multilang` — covers the new host languages added in CP4.
Guaranteed fallback: this repo's `atomic/` Go tree — an adversarial FP corpus
(contains SQL-keyword strings as regex patterns/comments that the gate must reject).

Outputs to `tmp/code-eval/out/embedded-sql/<corpus>/`.

Requires `bin/atomic` and `bin/embedded-sql-admission` (built automatically if absent).
`bin/embedded-sql-admission` is built from `atomic/cmd/embedded-sql-admission/`.

**Non-Go harvester limitation:** the admission tool's non-Go literal scanner
(`roughHarvestDoubleQuoted`) only extracts double-quoted single-line strings.
Single-quoted strings, triple-quoted strings, multi-line string literals, and
template literals (e.g. Python `f'...'`, TS `` `...` ``, multi-line heredocs) are
not harvested. As a result, the admission surface reported for the `multilang`
corpus and Python/TypeScript repos is an undercount of the literals that the
production embedded-SQL post-pass would actually evaluate. The adversarial FP
corpus (the local `atomic/` Go tree) is not affected — Go string literal
extraction uses the proper `HarvestGoStringLiterals` harvester. The referential
integrity check (step 2) is not affected by this limitation.
