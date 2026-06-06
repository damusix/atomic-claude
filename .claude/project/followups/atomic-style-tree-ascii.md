---
id: atomic-style-tree-ascii
title: Add tree + ASCII diagram format to atomic output style
created: "2026-06-06"
origin: |
    user request — wiki design session 2026-06-06
kind: plan
review_by: "2026-08-05"
status: open
---

User strongly prefers the **bullets + tree-structure + ASCII-flow** explanation format over dense prose — "much simpler to understand." Reference example: the wiki forcing-function proposal from the 2026-06-06 design session (terse decision bullets → indented tree of what-runs-where → ASCII flow diagram).

**Change:** extend `output-styles/atomic.md` (the "Diagrams and tables" section) so that when explaining a multi-part proposal, architecture, or sequenced flow, the style prefers:

1. terse decision bullets,
2. an indented tree showing hierarchy / what-runs-where,
3. an ASCII flow diagram for sequencing,

over long prose paragraphs. TUI replies stay ASCII-only (existing rule).

**Build note:** `output-styles/` is a bundled artifact — the change ships to every install. Run `make bundle` (and `make render` if any template is touched) per the build pipeline, and update `/atomic-help` only if the style's described behavior materially changes.


## Example of the target format

The format that prompted this follow-up — three parts, used together when explaining a multi-part proposal, architecture, or sequenced flow. (Worked example: the wiki forcing-function explanation the user approved.)

**1. Bullets — the decisions.** Terse, one line each, no throat-clearing:

```
- Hook detects neglect, never drift. One timestamp read per wiki. No git spawns.
- Drift detection stays on-demand (atomic wiki stale), only when the user acts.
- The nudge is cheap to clear, not a guilt trip — that's what beats banner blindness.
- reflects_rev is stamped in code, not the model.
```

**2. Tree — what runs where.** Indented hierarchy showing components and their inputs/outputs:

```
wiki freshness
├── session-start hook ............... DETECT neglect (cheap)
│   ├── input : <wikis> block in ~/.claude/CLAUDE.md
│   ├── per wiki: 1 stat — last-refresh timestamp
│   └── emit  : nudge if age > threshold OR dirty-marker set
├── ship boundary .................... DETECT drift (cheap, real)
│   ├── when  : a ship verb refreshes a member repo's signals
│   └── emit  : touch dirty-marker for that wiki
└── /refresh-wiki .................... HEAL (on demand, the only heavy step)
    ├── atomic wiki scan / stale  → membership + precise drift
    ├── incremental pass          → re-author only flagged/pending
    └── code stamps reflects_rev  + bump timestamp + clear marker
```

**3. ASCII — the flow.** Boxes and arrows for sequencing across actors:

```
  ship in a member repo                  open any session
  (signals already refreshing)           (session-start hook fires)
           │                                      │
  cwd under a <wikis> root?              for each wiki: stat refresh time
           │ yes                                  │
           ▼                                      ▼
   touch wiki dirty-marker ──────►  age > threshold? OR marker set?
                                                  │ yes
                                                  ▼
                          nudge: "wiki X stale — /refresh-wiki"
                                                  │ user runs it
                                                  ▼
                                          /refresh-wiki  (heal + clear)
```

**Rules of thumb the style should encode:**

- Use the tree when there are ≥3 components with a parent/child or input/output relationship.
- Use the ASCII flow when sequencing crosses actors or has branches/merges.
- Lead with bullets; reach for tree/ASCII when prose would need ≥2 paragraphs to say the same thing.
- TUI replies stay ASCII-only (no Mermaid in chat — that's for `docs/`).
