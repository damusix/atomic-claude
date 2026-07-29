# Design: declare repo and realm scope in `.claude/atomic.toml`


Source: [issue #172](https://github.com/damusix/atomic-claude/issues/172).


## Problem


Atomic answers "what am I inside of" three ways, and the realm answer is not a
fact about the directory — it is a fact about the user's `~/.claude/CLAUDE.md`.


| Question | Answered by | Kind of fact |
|---|---|---|
| what repo am I in | `repoctx.Resolve` → `git rev-parse --show-toplevel` | filesystem |
| does this repo have a wiki | `where.resolveRepoScope` → walk up for `docs/wiki/index.md` | filesystem |
| what realm am I in | `where.resolveRealmScope` → `<wikis>` block | per-user registration |


Consequences of the third row: the same directory is a realm for one user and
not another; a realm appears or vanishes because of config the session never
touched; and nothing committed to the tree records that it is a realm root, so
the fact is not reviewable and not shareable.


## Approach


`.claude/atomic.toml` — already a read-if-present, hand-authored, committed
config (`config.RepoConfigPath`, `LoadRepoConfig`, doctor check 13,
`[code] ignore`) — gains a top-level `scope` key that both init verbs write.
Nothing writes the file today, so this makes it the thing that declares
repo/realm identity.

    scope = "repo"    # or "realm"

Discovery puts the marker first and keeps every current mechanism as fallback,
so existing repos and realms work unchanged:

    repo root:   nearest .claude/atomic.toml with scope="repo",  else git toplevel
    realm root:  nearest .claude/atomic.toml with scope="realm", else <wikis> block


### The walk rule


Walking up from cwd you reach the repo marker before the realm marker, because
the repo sits inside the realm:

    ~/projects/taxgentic/.claude/atomic.toml           scope = "realm"
    ~/projects/taxgentic/server/.claude/atomic.toml    scope = "repo"
                         ^^^ cwd

So the rule is **first marker of the kind being asked for**, continuing past
markers of other kinds. "First marker wins" breaks realm discovery the moment a
repo marker sits between cwd and the realm root.

Getting this right means nested realms fall out later for free — "nearest
`scope="realm"` above cwd" is already the correct answer for a realm inside a
realm. Not implemented here, just not designed out.

The walk resolves each level through `config.RepoConfigPath`, so a non-default
`harness.dir` (e.g. `.pi`) is honored without a second code path. It does not
stop at a `.git` boundary: a realm root sits above member repos, so stopping at
`.git` would make realm markers unreachable from inside a member.


## Decisions


The issue left five decisions open. Each is settled here.


### 1. The marker outranks `<wikis>`


A marker-declared realm is a realm even when it is absent from the user's
`<wikis>` block. `<wikis>` keeps two jobs it is genuinely better at: the
session-start staleness nudge, and locating a realm's `wiki/index.md` for
member data.

The consequence the issue flagged — a realm can exist that the staleness nudge
does not know about — is correct behavior, not a leak. `<wikis>` is a per-user
list of wikis that user wants nagging about. Identity and subscription are
different facts and should not be carried by one mechanism.


### 2. Conflicts


| Situation | Resolution | Doctor |
|---|---|---|
| Marker says `realm`, `<wikis>` does not list the root | Marker wins; realm resolves | silent (decision 1) |
| Root is a `<wikis>` realm root, marker says `repo` | Marker wins; realm does not resolve there | WARN |
| `scope` value is neither `repo` nor `realm` | Marker ignored entirely; fall through to the existing mechanism | WARN |
| File is malformed TOML | Marker ignored; fall through | WARN (already today) |

An invalid or unparseable marker must never act as a marker of either kind —
otherwise a typo silently relocates a session's idea of its own root. Falling
through to the pre-existing mechanism is always a safe answer, because it is the
answer atomic gives today.

Layout-shape validation (`scope = "realm"` with no `wiki/`, `scope = "repo"`
with no `.git`) was considered and cut. Both shapes are legitimate: a realm is
marked before its first `/refresh-wiki` writes `wiki/`, and atomic explicitly
supports non-git trees (`repoctx` falls back to cwd by design). Warning on them
would fire on correct setups.


### 3. Backfill is manual


Both init verbs are idempotent, so re-running them is the backfill path.
`atomic migrate` does not do it automatically: `migrate` relocates per-user
state under `~/.atomic`, and writing into a user's tracked repo without asking
is not that verb's contract.

Discoverability rides on decision 4 instead — `atomic where` names the mechanism
that answered, and names the init verb when a realm resolved through the
registry rather than a marker. That puts the nudge where someone is already
asking the orientation question.


### 4. `where` reports repo root and provenance


`atomic where` is the orientation command and today reports neither the repo
root nor how it decided any axis. It gains a repo-root line and a provenance
token on both the repo-root and realm axes, in human and JSON output.

`where` carries a documented zero-git-subprocess contract, so its repo-root
fallback is an upward `.git` stat walk rather than `git rev-parse`. That
diverges from `repoctx` on submodules and `GIT_DIR` overrides. The divergence is
accepted and shrinks to nothing wherever a marker exists — which is the point of
the marker. It is stated in the package doc rather than papered over.


### 5. No optional `name`


Cut. Nothing consumes a display name today; the issue's own scope list omits it;
and #171's bus member naming reads `repoctx`/`where` through existing
signatures, so it gets the marker for free and can derive a name from the
basename. Adding an unread key invites drift between what is declared and what
is used.


## Non-goals


- **Nested realms.** Enabled by the by-kind walk, not implemented.
- **`codeintel/realm.Resolve`** keeps reading `<wikis>` unchanged. Code-index
  realm resolution additionally needs `<realm>/code.toml` members and
  `<realm>/.atomic/*.db`, which only exist along the registered path; honoring a
  bare marker there would resolve a realm root with no member registry and no
  databases — a worse answer than today's `ScopeNoIndex`.
- **Anything else `<wikis>` does** beyond precedence.
- **Bus member naming (#171).** Consumes this through existing signatures.
