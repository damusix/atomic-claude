# Spec: scope marker in `.claude/atomic.toml`


Design: [`docs/design/scope-marker.md`](../design/scope-marker.md). GitHub issue: #172.


## Contract


`.claude/atomic.toml` — resolved through `config.RepoConfigPath(root)`, so
harness-dir aware — gains one top-level key:

    scope = "repo"    # or "realm"

The key is optional. Its absence leaves every current behavior unchanged. When
present and valid it is authoritative for root discovery, outranking both
`git rev-parse --show-toplevel` and the `<wikis>` block in
`~/.claude/CLAUDE.md`.

Only `repo` and `realm` are valid values. Any other value, and any file that
fails to parse, is **not a marker of either kind**: discovery falls through to
the pre-existing mechanism, which is the answer atomic gives today.

`<wikis>` keeps its two other jobs unchanged — the session-start staleness
nudge, and locating a realm's `wiki/index.md` for member data. A marker-declared
realm absent from `<wikis>` therefore gets no staleness nudge. That is intended.


### Discovery order


    repo root:   nearest scope="repo" marker at or above cwd,  else git toplevel, else cwd
    realm root:  nearest scope="realm" marker at or above cwd, else <wikis> block

The walk takes the **first marker of the kind being asked for** and continues
past markers of other kinds — a repo marker between cwd and a realm root must
not terminate the realm walk. The walk runs to the filesystem root; it does not
stop at a `.git` boundary, because a realm root sits above its member repos.


## Checkpoints


| # | Deliverable | Done when |
|---|---|---|
| CP1 | `scope` in the repo-config schema; the by-kind upward walk | `RepoConfig.Scope` parses; `scope` is not reported as an unknown key; `FindScopeRoot` resolves both kinds under nesting |
| CP2 | Both init verbs write the marker | `atomic repo init` writes `scope = "repo"`; `atomic wiki init --scope <s>` writes `scope = "<s>"`; both idempotent and byte-preserving |
| CP3 | `repoctx` resolves repo root marker-first | Marker at or above cwd wins over git toplevel; provenance reported |
| CP4 | `where` reports repo root, realm marker-first, and provenance | New repo-root axis; realm resolves from a marker with no `<wikis>` entry; human + JSON carry a source token |
| CP5 | Doctor check 13 validates the marker | Invalid value WARNs; valid value appears in the PASS detail; marker/`<wikis>` contradiction WARNs |
| CP6 | Docs and discoverability | Reference docs describe the key and discovery order; render + bundle parity green |


### CP1 — schema and walk


In `atomic/internal/config`:

- `RepoConfig` gains `Scope string` with tag `toml:"scope"`.
- `checkUnknownRepoKeys` accepts `scope` as a known top-level **leaf**. It
  currently treats every top-level key as a table name, so without this change
  `scope` warns as unknown. Unknown-key behavior for every other key is
  unchanged.
- `ValidScope(s string) bool` reports whether `s` is `"repo"` or `"realm"`.
- `FindScopeRoot(startDir, scope string) (root string, found bool)` walks from
  `filepath.Clean(startDir)` upward to the filesystem root. At each level it
  reads `RepoConfigPath(dir)` and returns `dir` when that file parses and its
  `Scope` equals `scope`. A missing file, a parse error, an invalid `Scope`, or
  a `Scope` naming the other kind all continue the walk. It returns no error:
  discovery degrades, never fails.
- `ScopeSource` enumerates how a root was decided, with `String()` rendering the
  lowercase token used in output: `none`, `marker`, `git`, `registry`, `cwd`.

Tests must cover the nesting case from the design doc (a `scope="repo"` marker
between cwd and a `scope="realm"` root, both kinds resolving correctly from the
same cwd), an invalid value being ignored, and a malformed file being ignored.


### CP2 — both init verbs write the marker


One writer, in `atomic/internal/config`, used by both verbs — `repoinit` and
`wiki` both already may import `config` without a cycle.

    EnsureScopeMarker(root, scope string) (ScopeMarkerOutcome, error)

Outcomes: `created` (file did not exist), `added` (key inserted into an existing
file), `ok` (key already present with this value), `conflict` (key present with
a different value — the file is left untouched).

A conflicting marker is never rewritten. The declaration is the user's committed
statement about their own tree; silently flipping it is the failure mode this
whole feature exists to prevent. The caller surfaces the conflict and exits
non-zero.

**Insertion position is load-bearing.** `scope` is a top-level key, so it must be
written above the first `[table]` header. Appending at EOF would land it inside
`[code]` and parse as `code.scope`. On an existing file, a line counts as a
table header only when its trimmed form starts with `[` **and** it sits at
top-level statement position — the accumulated bracket depth of every
preceding line is zero. Depth is computed by counting `[` and `]` bytes
outside quoted strings (TOML basic `"..."` and literal `'...'`, honoring `\`
escaping inside basic strings only); this correctly skips an interior line of
a multi-line array (e.g. `[1, 2],` inside a multi-line array-of-arrays) while
still detecting an array-of-tables header (`[[name]]`). Insert the line
immediately before the first such header, or at EOF when the file has none.
The inserted line is terminated with the file's dominant existing line ending
(CRLF if CRLF outnumbers bare LF, else LF; a file with no line ending at all
gets LF). Every other byte is preserved: no reordering, no reformatting, no
comment loss.

Wiring:

- `repoinit.Init` gains a seventh guarantee, after the six existing ones,
  emitting an `Action` in the same shape (`created` / `ok`) so the existing
  output loop renders it unchanged. A `conflict` outcome is an error.
- `wikiInitAction` calls `EnsureScopeMarker(absRoot, scope)` with the `--scope`
  value it already validates, and reports it alongside the CLAUDE.md scaffold
  line. Its existing scaffold no-op behavior is unchanged.

Both stay idempotent: a second run reports `ok` and writes nothing.


### CP3 — `repoctx` marker-first


`repoctx.ResolveFrom(dir, override string) (string, config.ScopeSource, error)`
is the new full form:

1. `override` non-empty → absolute, existence-checked, `ScopeSourceNone`
   (unchanged behavior, explicit user instruction).
2. `FindScopeRoot(dir, "repo")` → `ScopeSourceMarker`.
3. `git rev-parse --show-toplevel` run in `dir` → `ScopeSourceGit`.
4. Otherwise `dir` → `ScopeSourceCwd`.

`Resolve(override string) (string, error)` is kept as a thin delegate over the
process cwd, discarding the source, so existing callers in `main.go` and
`codeintel/cli/realm.go` need no change.


### CP4 — `where`


`Report` gains a `RepoRoot RepoRootReport` field (`Path string`,
`Source config.ScopeSource`). Resolution:

1. `FindScopeRoot(cwd, "repo")` → `marker`.
2. Upward walk for a `.git` entry (directory or file) → that directory, `git`.
3. Otherwise cwd, `cwd`.

Step 2 is a stat walk, not a subprocess: `where` carries a documented
zero-git-spawn contract and keeps it. The package doc states the resulting
divergence from `repoctx` (submodules, `GIT_DIR` overrides) and that it
disappears wherever a marker exists.

`resolveRealmScope` tries `FindScopeRoot(cwd, "realm")` first. On a hit, the
realm root is that directory with `Source` `marker`; position is `root` when
`cwd` equals it, otherwise member/orphaned classification proceeds against
`<realm>/wiki/index.md` exactly as today, and degrades to `orphaned` when that
file is absent. On a miss, the current `<wikis>` path runs unchanged with
`Source` `registry`. `RealmScopeReport` gains the `Source` field;
`RealmNone` carries `none`.

The existing repo-scope-wiki axis (`docs/wiki/index.md`) and the `CodeIndex`
axis are unchanged.

Output — human gains a first line and two provenance tokens:

    repo root:        /path/to/root — marker
    repo-scope wiki:  found — /path/to/root/docs/wiki/index.md
    realm-scope:      root — /path/to/realm (registry)
    code-index scope: NoIndex

When a realm resolved through `registry`, human output appends one hint line
naming `atomic wiki init --scope realm` as the way to declare it. This is the
feature's only backfill affordance; it is absent from JSON.

JSON gains `repo_root` (`path`, `source`) and a `source` field on
`realm_scope`. Existing fields keep their names and shapes.


### CP5 — doctor check 13


`RunCheckRepoConfigWith` keeps its severity ceiling: PASS or WARN, never FAIL.
Two additions:

- `Scope` present and not valid → WARN naming the value and the two accepted
  values.
- `Scope` valid → included in the PASS detail, e.g.
  `.claude/atomic.toml ok (scope=repo, 1 ignore pattern(s))`.

`checkRepoConfig` additionally WARNs when `opts.RepoRoot` is a realm root
registered in `opts.ClaudeMDPath`'s `<wikis>` block while the marker says
`scope = "repo"` — two mechanisms making incompatible claims about one
directory. An empty `ClaudeMDPath` skips this sub-check. It is not run from
`RunCheckRepoConfigWith`, which stays root-only for existing callers and tests.

No layout-shape validation: a realm marked before its first `/refresh-wiki`,
and a non-git tree marked as a repo, are both legitimate (design decision 2).


### CP6 — docs and discoverability


- `docs/reference/code-intel.md` documents `scope` alongside `[code] ignore`
  where the file is already described.
- `docs/reference/concepts.md` documents the discovery order and the
  marker-outranks-`<wikis>` precedence.
- `docs/reference/wiki-workflow.md` notes that `atomic wiki init --scope realm`
  now declares realm identity, not only the CLAUDE.md scaffold.
- No new verb, flag, agent, skill, or command: `/atomic-help` needs no new row.
  The `where` and `repo init` rows already exist. The existing `wiki init` row's
  one-line description is updated in both `atomic/internal/cliusage/cliusage.go`
  and `templates/commands/atomic-help.md` to name the scope marker alongside
  the CLAUDE.md scaffold — CP2 changed what the verb does, so its description
  must change too.
- `cliusage` is unchanged — no flag or verb-path is added.
- `make render` and `make -C atomic bundle` run and their outputs are committed
  if any bundled artifact changed.


## Change tree


    atomic/
      internal/
        config/
          repo.go            M  RepoConfig.Scope; scope as a known top-level leaf
          scope.go           A  ValidScope, FindScopeRoot, ScopeSource, EnsureScopeMarker
          scope_test.go      A
        repoinit/
          repoinit.go        M  seventh guarantee: write scope = "repo"
          repoinit_test.go   M
        wiki/
          action.go          M  wikiInitAction writes the --scope value
          init_test.go       M
        repoctx/
          repoctx.go         M  ResolveFrom; Resolve delegates
          repoctx_test.go    M
        where/
          where.go           M  RepoRoot axis; realm marker-first; Source fields
          format.go          M  repo-root line, provenance tokens, JSON fields
          where_test.go      M
        doctor/
          checks_repo_config.go       M  scope validation; <wikis> contradiction
          checks_repo_config_test.go  M
    docs/
      reference/
        code-intel.md        M  the scope key alongside [code] ignore
        concepts.md          M  discovery order and precedence
        wiki-workflow.md     M  wiki init --scope declares identity


## Outline


- `atomic/internal/config/scope.go`
    - `ScopeSource` — how a root was decided
        - `String` — lowercase output token
    - `ScopeMarkerOutcome` — what `EnsureScopeMarker` did
    - `ValidScope` — is a string one of the two accepted values
    - `FindScopeRoot` — nearest marker of one kind at or above a directory
    - `EnsureScopeMarker` — idempotent, byte-preserving marker write
- `atomic/internal/config/repo.go`
    - `RepoConfig` — gains `Scope`
    - `checkUnknownRepoKeys` — top-level leaves are no longer assumed to be tables
- `atomic/internal/repoinit/repoinit.go`
    - `Init` — gains the seventh guarantee
- `atomic/internal/wiki/action.go`
    - `wikiInitAction` — writes the marker for its validated `--scope`
- `atomic/internal/repoctx/repoctx.go`
    - `ResolveFrom` — directory-parameterized resolution reporting provenance
    - `Resolve` — delegates over the process cwd
- `atomic/internal/where/where.go`
    - `RepoRootReport` — path plus provenance
    - `Report` — gains `RepoRoot`
    - `RealmScopeReport` — gains `Source`
    - `resolveRepoRoot` — marker, else `.git` stat walk, else cwd
    - `resolveRealmScope` — marker first, `<wikis>` fallback
- `atomic/internal/where/format.go`
    - `FormatHuman` — repo-root line, provenance tokens, registry backfill hint
    - `FormatJSON` — `repo_root` object, `realm_scope.source`
- `atomic/internal/doctor/checks_repo_config.go`
    - `RunCheckRepoConfigWith` — validates the value, reports it on PASS
    - `checkRepoConfig` — `<wikis>` contradiction sub-check


## Flows


**Writing a marker (`atomic repo init`, `atomic wiki init --scope <s>`)**

1. Verb resolves its root and calls `EnsureScopeMarker(root, scope)`.
2. File absent → create it holding only the scope line. Outcome `created`.
3. File present, key absent → find the first table header at bracket depth
   zero; insert the line immediately above it, or at EOF when there is none.
   Outcome `added`.
4. File present, key equals `scope` → write nothing. Outcome `ok`.
5. File present, key differs → write nothing. Outcome `conflict`; the caller
   reports it and exits non-zero.

**Resolving a root (`repoctx.ResolveFrom`, `where.Resolve`)**

1. `repoctx` only: a non-empty `override` short-circuits everything.
2. Walk upward from the start directory. At each level read
   `RepoConfigPath(dir)`; a file that parses and whose `Scope` equals the kind
   being asked for ends the walk at that directory, source `marker`.
3. A missing file, a parse error, an invalid value, or the other kind
   continues the walk. The walk ends at the filesystem root.
4. No marker → the pre-existing mechanism: `git rev-parse --show-toplevel`
   for `repoctx` (source `git`), a `.git` stat walk for `where`'s repo root
   (source `git`), the `<wikis>` block for `where`'s realm (source `registry`).
5. Nothing matched → cwd with source `cwd` for a repo root; `RealmNone` with
   source `none` for a realm.

**Reporting (`atomic where`)**

1. Resolve all four axes: repo root, repo-scope wiki, realm scope, code index.
2. Human output prints one line per axis, each carrying its provenance token.
3. A realm resolved with source `registry` appends one hint line naming
   `atomic wiki init --scope realm`. JSON carries no hint.

**Checking (`atomic doctor`, category 13)**

1. File absent → PASS, informational (unchanged).
2. `Scope` invalid → WARN naming the value and the two accepted values.
3. `Scope` valid → included in the PASS detail alongside the ignore-pattern count.
4. `Scope` is `repo` while the root is a `<wikis>`-registered realm root → WARN.


## Out of scope


- Nested realms (enabled by the by-kind walk, not implemented).
- `codeintel/realm.Resolve` — unchanged, still `<wikis>`-driven. See the design
  doc's non-goals for why a bare marker is a worse answer there.
- An optional `name` key (design decision 5).
- Automatic backfill via `atomic migrate` (design decision 3).


## Change log


- 2026-07-29 — initial spec from issue #172.

- 2026-07-29 — table-header detector and line-ending correction

  **What changed:** CP2's insertion-position rule now requires bracket depth
  zero (counted outside quoted strings) in addition to the trimmed-`[`-prefix
  check, so an interior line of a multi-line array is never mistaken for a
  table header. The rule also now specifies that the inserted line is
  terminated with the file's dominant existing line ending, not always LF.

  **Why:** review of the CP1+CP2 implementation found the trimmed-`[`-prefix
  check alone fires on an interior line of a multi-line array-of-arrays
  (e.g. `matrix = [\n  [1, 2],\n]`), splicing the scope line mid-array and
  producing unparseable TOML (`toml: incomplete number`). Not reachable
  through today's schema, but the heuristic itself was wrong. A second,
  cosmetic finding: the inserted line always used LF, so a CRLF-authored file
  got one LF line spliced in.

  **Superseded:** the prior body said to insert before "the first line whose
  trimmed form starts with `[`, or at EOF when the file has no table header" —
  no bracket-depth condition, no line-ending rule.

- 2026-07-29 — add the required Change tree / Outline / Flows sections

  **What changed:** the body gains `## Change tree`, `## Outline`, and
  `## Flows`, describing the full six-checkpoint scope.

  **Why:** `rules/specs/spec-currency.md` requires all three on specs drafted
  after that rule shipped. This spec was drafted after it and omitted them.

- 2026-07-29 — Correction: CP6's `wiki init` one-line description does not
  stay accurate on its own

  **What changed:** CP6 now says the `wiki init` one-line description in
  `atomic/internal/cliusage/cliusage.go` and `templates/commands/atomic-help.md`
  is updated to name the scope marker alongside the CLAUDE.md scaffold.

  **Why:** CP5+CP6 review found both descriptions still described only the
  CLAUDE.md scaffold after CP2 changed `wiki init` to also write the scope
  marker, violating `CLAUDE.local.md`'s help-router contract ("changing what
  an existing surface does, in a way that would alter its one-line
  description" is a non-negotiable trigger).

  **Superseded:** the prior CP6 body claimed the `where` and `repo init` rows
  "already exist and their one-line descriptions stay accurate" without
  naming that `wiki init`'s description needed a corresponding update.
