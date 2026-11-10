---
type: Domain
description: How a session starts already knowing this repo: a code scan, a model pass that writes the router and these domain files, and the @-ref that loads the router every turn.
tags: [context, codegen, agents]
---

# signals

## What it does

A Claude session in this repo starts already knowing the repo's shape (languages, build commands, where each feature lives) instead of spending its first turns discovering it. This domain is what puts that knowledge in context, and keeps it true as the code moves.

The kinds of file it writes, and the difference between them, are the whole design:

| File | Written by | When it loads |
|------|-----------|---------------|
| [`docs/wiki/scan.md`](scan.md) | code | never; too large to load into a session |
| [`docs/wiki/index.md`](index.md), the **router** | a model | every turn, via an `@`-ref in a [`CLAUDE.md`](../../CLAUDE.md)-family file |
| `docs/wiki/<domain>.md` | a model | on demand, when a task reaches that domain |
| `.claude/rules/wiki/<domain>.md`, a **pointer card** | a model | on demand, whenever a session touches a file matching that domain's `paths:` glob |

Only the router is `@-ref`'d, so the router alone is what every turn pays for. It points; detail lives one hop away in a domain file, such as this page. `scan.md` is the raw substrate underneath both: committed, read on demand by the inferrer, never loaded into a session. The pointer card never carries wiki content, only a link to the domain page and the domain's related docs, delivered by Claude Code's own `paths:`-scoped rule injection rather than an `@`-ref.

**The naming does not match the paths.** The domain, the Go package, and the CLI verb are all called `signals`, but nothing lands in a path containing that word. Output goes to [`docs/wiki/`](.) and [`.claude/rules/wiki/`](../../.claude/rules/wiki), and the command that drives it is `/refresh-wiki`. There is also a separate `wiki` domain: the same inferrer agent, pointed at a whole realm of repos instead of this one. See [`docs/wiki/wiki.md`](wiki.md).

## How it works

Code scans and linkifies; the model writes the router, domain pages, and pointer cards, and wires the `@-ref`. When a post-scan step wrote a file the scan reads, code scans once more so the committed `scan.md` matches the tree the refresh leaves behind.

```mermaid
flowchart TD
    A["atomic signals scan<br/>(code)"] --> B["docs/wiki/scan.md"]
    B --> C["atomic-wiki-inferrer<br/>(model)"]
    C --> D["docs/wiki/index.md<br/>+ one file per domain"]
    D --> R["Step 7b (model)<br/>.claude/rules/wiki/<br/>#lt;domain#gt;.md"]
    R --> E["Step 8 (model)<br/>@-ref wired into<br/>CLAUDE.md family"]
    E --> F["Step 8b (code)<br/>atomic signals linkify"]
    F -->|"7b or 8 appended<br/>to a scanned file"| H["Step 8d (code)<br/>re-scan + #lt;scan-sha#gt;<br/>re-stamp"]
    B -.prior copy.-> G["tmp/.scan.prev.md<br/>(gitignored)"]
```

### Staleness and the gate verbs

**Staleness is content-based, not mtime-based.** `Stale` reassembles the deterministic body exactly as `Scan` would and compares bytes. An idempotent regeneration that only bumps mtimes stays fresh. `Scan` reinforces this by skipping the write entirely when the body is unchanged.

Exit codes, shared by the gate verbs:

| Verb | 0 | 1 | 2 |
|------|---|---|---|
| `atomic signals stale` | fresh, silent | stale, prints evidence and directive | hard error, for example `scan.md` missing |
| `atomic signals diff` | no diff | diff present, written to stdout | no prior version, or hard error |

Exit 1 versus exit 2 is the distinction callers act on: "out of date" is a refresh trigger, "broken" is not. On exit 1 `stale` prints its evidence and a directive, imperative because the consumer is a model that can rationalize a bare exit code away:

```
signals: STALE — a fresh scan would change the deterministic snapshot (~N lines)
→ refresh required; dispatch atomic-wiki-inferrer. do not skip.
```

**`<scan-sha>` has exactly one job.** It catches a `scan.md` committed without a matching re-infer, forcing `scope = full`. No other staleness decision consults it; routine scope decisions use `git diff HEAD -- docs/wiki/scan.md` directly.

**`scan.md` is committed; `tmp/.scan.prev.md` is not.** The committed blob is the diff baseline both the scope computation and the `<scan-sha>` tiebreaker read. The `tmp/` copy is a gitignored fallback for environments without git.

**Step 8d re-scans only after a post-scan write the scan can see.** It runs when Step 7b appended a negation to an ignore file or Step 8 appended the `@-ref` block, and no other time, so source edits made during the run stay out of a scan the inferrer never read. It runs `atomic signals scan` again and recomputes `<scan-sha>` in [`docs/wiki/index.md`](index.md) with `git hash-object docs/wiki/scan.md`; the re-stamp leaves the scan unchanged because [`docs/wiki/`](.) is scan-excluded. A failing re-scan has its error quoted, leaves `<scan-sha>` as Step 7 wrote it, and the run continues to Step 9. Step 9's `<N> sections changed` comes from `git diff HEAD -- docs/wiki/scan.md`, reads `unknown` without git, and never comes from `tmp/.scan.prev.md`, which the re-scan can replace.

### What the scan walks

**`.signalsignore` lines are either a plain exclude glob or a `+`-prefixed generated marker.** A plain line excludes and the file vanishes from the tree; a `+`-prefixed line marks the file `[generated]`, so it still appears with metadata but the inferrer skips it for domain content. Each glob is matched against both the repo-relative path and the bare filename, so `gen.go` matches `gen.go` and `dir/gen.go` alike.

**Language stats are extension-only and capped at ten.** No content sniffing, and a language absent from the table is either below the top ten by LOC or has no mapped extension.

### Pointer cards (Step 7b)

After [`docs/wiki/index.md`](index.md) is assembled (Step 7 of the repo pipeline), the inferrer emits one `.claude/rules/wiki/<domain>.md` card per domain (Step 7b), before it checks the `@-ref` wiring (Step 8). Which domains get a rewritten card, and which cards get deleted, follows a scope decision made once per run:

```mermaid
flowchart TD
    S["Step 7b"] --> Q1{".claude/rules/wiki/<br/>absent or no cards?"}
    Q1 -->|yes| PART["Step 3 partition,<br/>every domain<br/>(no atomic-wiki-writer)"]
    PART --> ALL["write every card<br/>(bootstrap)"]
    Q1 -->|no| Q2{"scope (Step 2b)"}
    Q2 -->|full| ALL
    Q2 -->|incremental| SOME["rewrite cards of<br/>re-dispatched domains"]
    ALL --> DEL["delete cards absent<br/>from index.md Domains"]
    SOME --> DEL
```

A repo whose wiki predates pointer cards takes the bootstrap branch on its first post-upgrade refresh even if that refresh is otherwise incremental, so it never gets stranded with cards for only the domains that happened to be re-dispatched. The bootstrap branch runs Step 3's domain partition for every domain in the router table before emitting anything, so every domain has a `<source_paths>` block to derive its card's globs from. There is no fallback to the router table's Start-here directory or any other approximation; if `<source_paths>` is still unavailable for a domain after the partition, that is a pipeline error to report, not a guess.

**Card contract.** `paths:` frontmatter globs come from that domain's own Step 4 `<source_paths>` block only: a directory becomes `dir/**`, a file stays a file. A page's `## Where it lives` table and `## Coupling` section are never glob sources. The generated-marker comment sits inside the frontmatter block, not the body, because the harness strips frontmatter from injection but re-injects the body on every matching read. The body is a typed pointer index: the router's one-line domain description verbatim, including any inline code spans it carries (for example `~/.claude`), then `Map:` (exactly one link, to that domain's wiki page), then whichever of `Contracts:` ([`docs/spec/`](../spec)), `References:` ([`docs/reference/`](../reference)), `Guides:` ([`docs/guides/`](../guides)), `Research:` ([`docs/research/`](../research)), `Designs:` ([`docs/design/`](../design)), or `Related:` (catch-all) holds at least one link. Every collection, `paths:` included, is a label on its own line with one `- entry` per line nested beneath it, so an added glob or doc shows as a one-line diff. Link candidates come from the domain page's own doc links plus doc surfaces the project's [`CLAUDE.md`](../../CLAUDE.md) couples to the domain; each is checked to exist on disk, dropped if missing, and sorted by path in `LC_ALL=C` byte order within its category. A fixed literal closing line ends every card. The body holds at most twelve links, and its length is `5 + categories + links` lines (24 at the ceiling), so an unchanged domain regenerates byte-identical and a refresh commit carries no wording churn.

**Ignore-file negation.** Before writing, the step runs `git check-ignore -v .claude/rules/wiki`. A match gets `!/rules/wiki/` and `!/rules/wiki/**` appended to the ignore file the probe names; once the negation lands, later probes report "not ignored" and skip the append. This repo's own [`.claude/.gitignore`](../../.claude/.gitignore) ignores `/rules/*` and already carries negation pairs for both `/rules/authoring/` and `/rules/wiki/`, so the probe reports "not ignored" here and the append step is a no-op.

**Report.** Interactive mode folds cards emitted, cards deleted, and any ignore-file edit onto the same Step 9 summary line as the section counts; silent mode returns nothing, so `signals-gate` (see Coupling) detects the ignore-file edit itself rather than reading it from inferrer output.

## Where it lives

| Path | Role |
|------|------|
| **Entry point and orchestration** | |
| [`context/commands/refresh-wiki.md`](../../context/commands/refresh-wiki.md) | `/refresh-wiki`, the only entry point, idempotent across first run and refresh. Repo scope runs R1-R8, and R8 folds the inferrer's card-emission summary onto its own report line. Realm scope runs a separate pipeline. |
| [`context/agents/atomic-wiki-inferrer.md`](../../context/agents/atomic-wiki-inferrer.md) | The orchestrator. Detects repo vs realm scope, loads the matching pipeline reference, sub-dispatches `atomic-wiki-writer` per domain and `atomic-reviewer` per domain file, then assembles the router and emits the pointer cards. Writes nothing outside the active wiki root, [`.claude/rules/wiki/`](../../.claude/rules/wiki), an ignore file's rules/wiki negation append, and the single `@-ref` target. |
| [`context/skills/atomic-wiki/references/repo.md`](../../context/skills/atomic-wiki/references/repo.md) | The repo-scope pipeline the inferrer executes (Steps 1-9, including 7b, the pointer-card step). The inferrer reads it from the installed path `~/.claude/skills/atomic-wiki/references/repo.md`, not from this repo. |
| [`context/agents/atomic-wiki-writer.md`](../../context/agents/atomic-wiki-writer.md) | Authors one domain page from source. Carries the `atomic-writing` skill in frontmatter rather than being asked to invoke it, and holds no `Agent` tool. |
| **Scan, staleness, and linkify (Go)** | |
| [`atomic/cmd/atomic/cmd_signals.go`](../../atomic/cmd/atomic/cmd_signals.go) (`runSignals`) | Verb dispatch and the process exit codes for `scan`, `show`, `stale`, `diff`, `linkify`. |
| [`atomic/internal/signals/signals.go`](../../atomic/internal/signals/signals.go) | `Scan` / `Stale` / `Diff` / `Show` / `LinkifyFiles`. Owns the [`docs/wiki/scan.md`](scan.md) and `tmp/.scan.prev.md` paths, `.signalsignore` parsing, and the `ErrStale` / `ErrDiffPresent` / `ErrNoPrior` sentinels. |
| [`atomic/internal/signals/tree.go`](../../atomic/internal/signals/tree.go) | File enumeration and the depth-capped tree render. Owns `skippedPrefixes`, `defaultMaxDepth`, and the `git ls-files` versus `WalkDir` split. |
| [`atomic/internal/signals/languages.go`](../../atomic/internal/signals/languages.go) | Extension-to-language map and per-language LOC totals. |
| [`atomic/internal/signals/manifests.go`](../../atomic/internal/signals/manifests.go) | Manifest parsers for `Cargo.toml`, `Gemfile`, `composer.json`, `go.mod`, [`package.json`](../../package.json), `pom.xml`, `pyproject.toml`, `requirements.txt`, found anywhere in the tree. |
| [`atomic/internal/signals/diff.go`](../../atomic/internal/signals/diff.go) | `atomic signals diff`: `git diff` inside a repo, unix `diff` against `tmp/.scan.prev.md` outside one. |
| [`atomic/internal/cliusage/cliusage.go`](../../atomic/internal/cliusage/cliusage.go) | The `signals` verb entries. `atomic validate artifacts` lints artifact citations against this table, so a verb or flag added here and nowhere else silently passes the lint. |
| **Gates and health checks** | |
| [`context/_partials/signals-gate.md`](../../context/_partials/signals-gate.md) | The `signals-gate` partial composed into ship verbs. Owns the docs-only guard, the `atomic signals stale` exit-code routing, and staging [`.claude/rules/wiki/`](../../.claude/rules/wiki) plus any modified or untracked [`.gitignore`](../../.gitignore), [`.claude/.gitignore`](../../.claude/.gitignore), or [`CLAUDE.md`](../../CLAUDE.md) (the Step 8 `@-ref` target) after a silent refresh. |
| [`atomic/internal/doctor/checks_signals.go`](../../atomic/internal/doctor/checks_signals.go) | Doctor category 3 (`signals`). Freshness plus router integrity. All WARN, never FAIL. Does not yet validate [`.claude/rules/wiki/`](../../.claude/rules/wiki) card files; tracked in [`.claude/project/followups/wiki-rules-doctor-validation.md`](../../.claude/project/followups/wiki-rules-doctor-validation.md). |
| [`atomic/internal/doctor/checks_refs.go`](../../atomic/internal/doctor/checks_refs.go) | Doctor category 4 (`refs`). The only FAIL-severity check in this domain. |
| **Contracts: lifecycle and timing** | |
| [`docs/spec/signals-workflow.md`](../spec/signals-workflow.md) | End-to-end lifecycle: files produced, the stale gate, inferrer dispatch, `@-ref` wiring rules, the `signals-gate` probe. Canonical for the agent and the command. |
| [`docs/spec/signals-refresh-timing.md`](../spec/signals-refresh-timing.md) | When the refresh fires and how the inferrer is scoped. C1 `changed_range`, C2 docs-only guard, C3 implementation-loop finalize, C4 autopilot, C5 wiring surfaces. |
| [`docs/design/signals-refresh-timing.md`](../design/signals-refresh-timing.md) | Why the primary refresh sits at implementation finalize and why `atomic signals stale` is the coordinator instead of a marker file. |
| [`docs/reference/repo-wiki.md`](../reference/repo-wiki.md) | User-facing account of when a refresh fires, and the "Pointer rules" section covering card shape, location, and regeneration. |
| **Contracts: router, layout, and linkify** | |
| [`docs/spec/signals-router.md`](../spec/signals-router.md) | Router shape, domain file layout, incremental versus full mode, the domain split budget, naming continuity. |
| [`docs/design/signals-router.md`](../design/signals-router.md) | Why a flat eager-loaded file fails at scale, and the domain-partitioning answer. |
| [`docs/spec/wiki-storage-relocation.md`](../spec/wiki-storage-relocation.md) | The [`docs/wiki/`](.) layout and which staging steps include `scan.md`. |
| [`docs/spec/signals-wiki-linkify.md`](../spec/signals-wiki-linkify.md) | Linkify contract: disk resolution as the filter, the skip-set, gitignore layer, idempotence, fenced blocks untouched. |
| **Contracts: pointer cards** | |
| [`docs/spec/wiki-pointer-rules.md`](../spec/wiki-pointer-rules.md) | Step 7b contract: scope branches (bootstrap, full, incremental), card frontmatter and typed-index shape, the ignore-file negation probe, and the report line. |
| [`docs/design/wiki-pointer-rules.md`](../design/wiki-pointer-rules.md) | Why pointer cards ride Claude Code's `paths:`-scoped rules instead of a Go generator or a single catch-all rule, and the evidence behind Approach A. |
| **Contracts: unimplemented** | |
| [`docs/spec/signals-project-detection.md`](../spec/signals-project-detection.md) | Framework and dependency annotations on manifest bullets. Unimplemented: `manifests.go` has no detection rules, and the signals package never reads [`.claude/atomic.toml`](../../.claude/atomic.toml). The spec also predates the current layout, naming `deterministic-signals.md` and [`.claude/project/`](../../.claude/project). |

## Constraints

**[`docs/wiki/`](.) is excluded from its own scan, and removing that exclusion wedges the stale gate permanently.** The inferrer writes a `<scan-sha>` of `scan.md` into `index.md`, which changes `index.md`'s blob SHA. If [`docs/wiki/`](.) were walked, that change would alter the scan tree, which would alter `scan.md`, which would make `atomic signals stale` return exit 1 forever. `skippedPrefixes` in `tree.go` holds the fixed [`docs/wiki/`](.) prefix alongside the harness-relative `ScratchpadDir` and `ProjectDir` prefixes.

**[`.claude/rules/wiki/`](../../.claude/rules/wiki) is excluded for the same reason.** The cards hold no source; Step 7b derives them from the scan, so walking them would make the scan depend on its own output, just as walking [`docs/wiki/`](.) would through the `<scan-sha>` stamp. `skippedPrefixes` holds the fixed [`.claude/rules/wiki/`](../../.claude/rules/wiki) prefix next to [`docs/wiki/`](.), and the exclusion applies in both the `git ls-files` and `WalkDir` enumeration modes.

**Write backtick paths, never markdown links and never `@-refs`.** An `@-ref` in the router eager-loads its target on every turn. A hand-written link skips linkify's on-disk check, so a path that stops resolving stays linked. `atomic signals linkify` renders resolvable tokens into relative links afterward, skipping `scan.md`, [`CLAUDE.md`](../../CLAUDE.md), and fenced code blocks, and rewriting a file only when the content changed. A token that does not resolve on disk is left as plain text, which is how `` `git status` `` and `` `atomic signals scan` `` survive. A token built only from `.` and `/` is left alone too, because it would otherwise resolve to the repo root and link there.

**The `--out` flag help names the wrong destination.** Both registrations in [`atomic/cmd/atomic/cmd_signals.go`](../../atomic/cmd/atomic/cmd_signals.go) (the cobra `scan` subcommand and the `signals-scan` flag set in `runSignals`) read `write substrate to <dir> instead of <root>/.claude/project/`, while the flag writes `<dir>/docs/wiki/scan.md`. A reader following `--help` looks under `<dir>/.claude/project/` and finds nothing.

**Pointer cards are wholly pipeline-owned.** A hand edit survives until the next full refresh or the next incremental refresh that re-dispatches its domain. There is no doctor check yet confirming a card's globs still match its domain's real `source_paths`, or that its links still resolve, between refreshes; that gap is the deferred [`wiki-rules-doctor-validation`](../../.claude/project/followups/wiki-rules-doctor-validation.md) follow-up.

**`paths:` frontmatter has no exclusion syntax, so the per-domain partition behind a card's globs has to be disjoint at build time, not enforced by excluding one domain's files from another's card.** Rung 1 of the tie-break (Start-here directory as longest path prefix) is mechanical and reproduces the same assignment on every refresh; rungs 2 and 3 (`## How it works` ownership, then narrower `source_paths`) are judgment calls the inferrer makes fresh each run, so a file resolved only by rung 2 or 3 is not guaranteed to land on the same domain's card across refreshes.

**A failed scan ends the run before inference, not the commit.** When `atomic signals scan` exits non-zero, the pipeline quotes its error and ends without inferring, and the commit that triggered it proceeds. That commit lands with `scan.md` and the wiki still stale, so the next `atomic signals stale` exits non-zero again.

## Coupling

**workflow** owns when the refresh fires. `/subagent-implementation` finalize and `/autopilot` Phase 4 each dispatch the inferrer once with `mode: silent` and `changed_range: <loop-base>..HEAD`, committed as `chore(signals): refresh after <topic>`. Both stage `docs/wiki/*.md`, the per-domain pointer cards under [`.claude/rules/wiki/`](../../.claude/rules/wiki) (guarded on the directory existing), and any of [`.gitignore`](../../.gitignore), [`.claude/.gitignore`](../../.claude/.gitignore), or [`CLAUDE.md`](../../CLAUDE.md) that `git status --short` reports modified or untracked, since the silent inferrer reports no paths and may have amended an ignore file or appended the `@-ref`. This matches the `signals-gate` partial's own staging step, so pointer-card writes or deletions, ignore-file edits, and the `@-ref` append from the implementation loop, autopilot, and ship-verb refresh paths land in that path's own commit. Change the inferrer's dispatch interface and each of those refresh paths changes with it.

**doctor** consumes contracts from `checks_refs.go` and `checks_signals.go`. `checks_refs.go` hardcodes `signalsRef = "@docs/wiki/index.md"` and the `candidateFiles` search order ([`claude.local.md`](../../claude.local.md), [`CLAUDE.local.md`](../../CLAUDE.local.md), [`CLAUDE.md`](../../CLAUDE.md), [`claude.md`](../../claude.md)). `checks_signals.go` parses the router's Domains table to detect missing and orphan domain files, reading the **last** content column of each row. Changing the router's column layout breaks that parser. It does not yet parse [`.claude/rules/wiki/`](../../.claude/rules/wiki) at all.

**config** supplies `output.signals.max_depth` from `~/.atomic/config.toml`, and `config.ScratchpadDir` / `config.ProjectDir` feed the scan's skip prefixes, so a non-default `harness.dir` changes what the scan walks.

**bundle** ships the inferrer, the command, and the `signals-gate` partial as source files directly under [`context/`](../../context): [`context/agents/atomic-wiki-inferrer.md`](../../context/agents/atomic-wiki-inferrer.md), [`context/commands/refresh-wiki.md`](../../context/commands/refresh-wiki.md), [`context/_partials/signals-gate.md`](../../context/_partials/signals-gate.md). A single `make bundle` expands any `{{ template ... }}` directive into the embedded bundle; nothing is written back into [`context/`](../../context), so editing any of them is editing the shipped source directly.

**wiki** shares the inferrer. The same agent serves repo scope here and realm scope there, branching on dispatch args. Pointer-card emission (Step 7b) is repo-scope only; realm scope has no equivalent.

**code-intel** is an optional input. `/refresh-wiki` R4 syncs a warm index or builds a cold one before dispatch, so domain clustering can use real import and call edges. Neither failure blocks the refresh. A failed warm sync prints a warning and the refresh proceeds on the stale index; a failed cold index prints a warning and the inferrer falls back to filename/path heuristics.
