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
`[code]` and parse as `code.scope`. On an existing file, insert the line before
the first line whose trimmed form starts with `[`, or at EOF when the file has
no table header. Every other byte is preserved: no reordering, no reformatting,
no comment loss.

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
  The `where` and `repo init` rows already exist and their one-line descriptions
  stay accurate.
- `cliusage` is unchanged — no flag or verb-path is added.
- `make render` and `make -C atomic bundle` run and their outputs are committed
  if any bundled artifact changed.


## Out of scope


- Nested realms (enabled by the by-kind walk, not implemented).
- `codeintel/realm.Resolve` — unchanged, still `<wikis>`-driven. See the design
  doc's non-goals for why a bare marker is a worse answer there.
- An optional `name` key (design decision 5).
- Automatic backfill via `atomic migrate` (design decision 3).


## Change log


- 2026-07-29 — initial spec from issue #172.
