# Wiki bucket argument hardening


## Goal


A help probe can never mutate state: `-h` / `--help` on any `atomic wiki bucket` verb prints
usage and exits 0 without writing. Bucket names that are not safe single path segments are
rejected at registration rather than created.


## Non-goals


- Migrating the wiki verbs to pflag/Cobra flag registration. That is the CLI-layer
  consolidation tracked by issue #130 (and gated behind #131); this spec hardens the
  hand-rolled parsers in place.
- Renaming, revalidating, or migrating already-registered buckets. Validation is
  register-time only.
- Issues #157 (`wiki stale` summary derivation) and #158 (`wiki stamp` flag order) — separate
  defects in the same package, out of scope here.


## Success criteria


- [ ] `atomic wiki bucket add -h` prints usage to stdout, exits 0, and creates nothing: no
      realm-root directory, no `wiki/.buckets/<name>/` manifest dir, no `<bucket>` entry in
      `wiki/index.md`, no `## Capture surfaces` bullet in the realm `CLAUDE.md`
- [ ] `atomic wiki bucket add --help` behaves identically
- [ ] `-h` and `--help` behave identically for every bucket sub-verb: `list`, `diff`,
      `promote`, `doc`, `skill`, `index`
- [ ] an unrecognized single-dash token (e.g. `-x`) is rejected with an error naming the
      token and exits 2 — parity with the existing `--x` behavior
- [ ] `RegisterBucket` rejects an empty name, a name beginning with `-`, a name containing a
      path separator, `.`, `..`, and the reserved name `wiki`
- [ ] every currently valid bucket name still registers — no back-compat break
- [ ] `go test ./...` green, except the pre-existing `internal/doctor`
      `TestRepairPlan_configWARN_fixable` environment failure (reads the real `~/.atomic`
      config; tracked by follow-ups `doctor-config-test-reads-real-home` and
      `dev-install-version-fails-doctor`)


## Approaches


No design doc: the defect and its remedy are prescribed by issue #164 and confirmed by
reading both parsers. The only open choice was where the help intercept lives.

| # | Approach | Pros | Cons |
|---|----------|------|------|
| 1 | Sentinel error from the shared scanner (`errUsageRequested`, mirroring `flag.ErrHelp`); each action prints its own usage and returns 0 | one intercept covers every caller; each verb keeps its specific usage text; Go-idiomatic | each action needs a short check branch |
| 2 | Intercept in `wikiBucketAction` before dispatch, print generic bucket usage | single site | wrong usage text per verb, and misses callers that bypass the dispatcher |
| 3 | Migrate the bucket verbs to pflag now | fixes the defect class permanently | that is issue #130's CLI-layer work; far too wide for a bug fix |


## Recommendation


**Approach 1**, plus independent name validation in `RegisterBucket` — defense in depth: the
parser guard protects the CLI path, the validation protects programmatic callers.

`parseBucketDocArgs` currently duplicates `resolveWikiRoot`, differing only in its `--router`
boolean. That duplication is why the defect has two sites. Collapse it to consume `--router`
and delegate the rest to `resolveWikiRoot`, so the hardening lands in exactly one scanner.


## Change tree


    atomic/internal/wiki/
    ├── action.go .............. M  (resolveWikiRoot: help sentinel + single-dash reject;
    │                                parseBucketDocArgs: delegate; 7 actions handle sentinel)
    ├── action_test.go ......... A  (scanner table tests; also pins the --root --router delta)
    ├── bucket.go .............. M  (RegisterBucket: name validation)
    ├── bucket_cli_test.go ..... M  (help-probe writes nothing, per mutation site)
    └── bucket_test.go ......... M  (name validation + back-compat cases)
    docs/reference/wiki-workflow.md  M  (bucket name rules + help note)


## Outline


    atomic/internal/wiki/action.go
      errUsageRequested — sentinel signalling a help probe, checked by callers
      resolveWikiRoot — reject any unrecognized dash-prefixed token; return the
                        sentinel for -h/-help/--help
      parseBucketDocArgs — consume --router, delegate remaining args to resolveWikiRoot
      wikiBucketAddAction — on sentinel: print usage to out, return 0
      wikiBucketListAction / DiffAction / PromoteAction / DocAction / SkillAction /
        IndexAction — same sentinel branch

    atomic/internal/wiki/bucket.go
      validateBucketName — reject empty, leading dash, path separator, dot-names, reserved
      RegisterBucket — call validateBucketName before any filesystem write

    atomic/internal/wiki/action_test.go
      help token yields sentinel — for -h, -help, --help
      unknown single-dash token rejected — parity with double-dash
      --router still parsed in any position — delegation preserves behavior

    atomic/internal/wiki/bucket_cli_test.go
      help probe writes nothing — asserts all four mutation sites untouched, per verb

    atomic/internal/wiki/bucket_test.go
      RegisterBucket rejects unsafe names — table of rejected inputs
      RegisterBucket accepts ordinary names — back-compat guard

    docs/reference/wiki-workflow.md
      Bucket names — allowed shape and what is rejected


## Flows


    Flow: help probe on a writing verb
    1. user runs `atomic wiki bucket add -h`
    2. resolveWikiRoot classifies `-h` as a help request -> returns errUsageRequested
    3. wikiBucketAddAction matches the sentinel -> prints usage to out, returns 0
    4. no RegisterBucket call, no mkdir, no index splice, no CLAUDE.md append

    Flow: unsafe name reaching registration
    1. a caller invokes RegisterBucket with a name beginning with `-`
    2. validateBucketName rejects it -> error returned before manifestDir is touched
    3. no manifest dir is created; the caller reports the error and exits non-zero


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Harden the shared scanner: help sentinel, single-dash rejection, `parseBucketDocArgs` delegation, sentinel branch in all 7 bucket actions | `internal/wiki/action.go`, `action_test.go` | atomic-implementer (mode: feature) | ~2 | `go test ./internal/wiki/` — help tokens yield usage + exit 0; `-x` rejected exit 2; `--router` unchanged |
| 2 | Validate bucket names in `RegisterBucket`; assert the help probe leaves all four mutation sites untouched; pin the `--root --router` delta | `internal/wiki/bucket.go`, `bucket_test.go`, `bucket_cli_test.go`, `action_test.go` | atomic-implementer (mode: surgical) | 1-2 | `go test ./internal/wiki/` — unsafe names rejected, ordinary names still accepted |
| 3 | Document bucket name rules and the help behavior | `docs/reference/wiki-workflow.md` | atomic-implementer (mode: surgical) | 1 | prose matches shipped validation; no stale claim |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Name validation rejects a bucket name already registered in a user realm | low | Validation is register-time only; existing manifests are never revalidated. Rules reject only unsafe shapes (leading dash, separators, dot-names), not ordinary kebab-case or underscored names. Back-compat guard test in checkpoint 2. |
| Returning exit 0 for help changes script behavior for callers that relied on the old non-zero probe | low | The old behavior on `-h` was exit 0 *with a side effect*; on `--help` it was exit 2. Nothing can have depended on the destructive path. Exit 0 for an explicit help request is the conventional contract. |
| Delegating `parseBucketDocArgs` silently changes `--router` handling | low | Dedicated test asserting `--router` is still honored in any position, including before and after positionals. |


## Implementation log


- 2026-07-25 — Built in three checkpoints on `fix/wiki-bucket-arg-hardening`, each reviewed by
  a fresh `atomic-reviewer`, then squashed to a single commit and rebased onto `origin/next`
  at `a32356e`. The per-checkpoint commits no longer exist in history; the sequence is
  recorded here.

  | CP | Scope | Verdict |
  |----|-------|---------|
  | 1 | scanner hardening: sentinel, dash-token rejection, `parseBucketDocArgs` delegation, 7 sentinel branches | PASS (1 nit) |
  | 2 | `validateBucketName` + four-mutation-site and `--root --router` pin tests | PASS (1 nit) |
  | 3 | `docs/reference/wiki-workflow.md` name rules + help note | PASS (final branch review, 0 findings) |

  Both nits were resolved in-run rather than deferred: CP1's flagged the `--root --router`
  behavior delta, pinned by a test in CP2; CP2's flagged that the pin test fell outside the
  declared file list, resolved by correcting this spec's Change tree and Checkpoints table.

  Re-verified after the rebase: the new base carries `a32356e` (`wiki stale` summary
  resolution, issue #157), which touches `stale.go` where this work touches `action.go` and
  `bucket.go` — no code overlap, and the rebase was conflict-free.

  Verified end-to-end against a built binary in a throwaway realm, not by unit tests alone:
  `-h` and `--help` print usage and exit 0 on all seven bucket verbs with all four mutation
  sites untouched; `-x` exits 2; ordinary names still register; `../escape`, `.`, `..`, and
  `wiki` are rejected leaving no manifest dir. `go test ./...`, `go vet`, `gofmt`,
  `go build`, and render/bundle parity all clean, with the one pre-existing
  `internal/doctor` environment failure unchanged.

  Note on layering: a leading-dash name never reaches `RegisterBucket` through the CLI — the
  scanner rejects it first with exit 2. `validateBucketName`'s leading-dash rule is therefore
  a backstop for programmatic callers, and is exercised directly by
  `TestRegisterBucket_UnsafeNamesRejected` rather than through the CLI path.

  Filed in passing, unrelated to this defect: `atomic-validate-inspects-zero-files` —
  `atomic validate` reports `0 PASS, 0 WARN, 0 FAIL` and inspects nothing, so ship verbs
  gate on a vacuous exit 0. Pre-existing; reproduced on the base branch.


## Change log


<!-- Amendments after approval get an entry here. The body above always describes current
     truth; this log records how it got there. -->

- 2026-07-25 — Initial spec. Derived from issue #164. Scope covers both hand-rolled parsers
  (`resolveWikiRoot` and `parseBucketDocArgs`), which the issue described as one.
- 2026-07-25 — Correction: checkpoint 2 also touches `action_test.go`, and the change tree
  marks that file `A` rather than `M`. Collapsing `parseBucketDocArgs` into a delegate made
  `--root --router` an error instead of a silent `root="--router"`; that delta is deliberate
  and is pinned by a test in `action_test.go`, so checkpoint 2's file list now names it.
