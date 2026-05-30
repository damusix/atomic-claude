# Typed follow-ups: findings vs plans

## Goal

Add a `kind` field (`finding` | `plan`) to follow-up ledger entries. Plans
surface every session as a backlog, link to specs, and are exempt from staleness
nagging; they are filed with `atomic followups add --kind plan`. Back-compatible:
existing entries read as `finding`. Closes GitHub issue #28.

## Non-goals

- **An auto-firing capture skill.** A conversational deferral-detector skill was
  considered (and briefly built) during this work and then cut — see
  `docs/design/typed-followups.md`. Capture is manual via the CLI; do not
  re-add an auto-fire skill without a fresh, explicit decision.
- A new surface (`BACKLOG.md`) or subsystem (`todos/`) — the ledger is the home.
- Reworking `/follow-up review` staleness beyond exempting `plan` entries.
- Wiring `/remind-me` into the ledger (it stays the timed surface).
- A structured dependency graph between plans.

## Success criteria

- [ ] Entry frontmatter supports `kind: finding | plan`; a missing `kind` parses as `finding` (no migration of existing entries).
- [ ] `atomic followups add --kind plan` writes `kind: plan`; `--kind` defaults to `finding`; `--severity` is optional when `--kind plan`, still required for `finding`.
- [ ] Invalid `--kind` value → exit 1 with a clear error (mirrors severity validation).
- [ ] `INDEX.md` render places `kind: plan` entries in a `## 📋 plans` section before the severity groups; each plan row shows its title and a `→ docs/spec/x.md` link when the entry has a `file` reference; plans are excluded from the severity buckets.
- [ ] `plan` entries are excluded from `/follow-up review` staleness flagging (queued, not stale) — at the `isStale` seam so render stale-count, `list --stale`, and `/follow-up review` all inherit it.
- [ ] `atomic followups list --json` output includes `kind` for each entry.
- [ ] `CLAUDE.md` "Where things live" follow-ups entry names both kinds and the manual `atomic followups add --kind plan` capture path.
- [ ] `make render` + `make bundle` parity clean; `go test ./...`, `go vet ./...`, `gofmt -l` clean; `atomic doctor` no new WARN/FAIL.

## Approaches

| # | Approach | Sketch | Cost | Risk |
|---|----------|--------|------|------|
| A | Typed ledger: `kind` field + plans section + manual CLI capture | one field + render branch | low-med | Go CLI/render change, bounded |
| B | Separate `BACKLOG.md` surface | second `@`-ref file | med | third surface to chart; splits deferred work |
| C | Full `todos/` subsystem | clone followups stack | high | duplicates machinery (axiom 2) |
| D | Docs-only note | document the gap | low | does not fix the leak |

An auto-firing capture skill was evaluated as a sub-component of A and rejected;
the reasoning lives in the design doc.

## Recommendation

**A, with manual CLI capture.** One ledger, typed — resolves both the existing
finding/plan mix and the missing home for deferred specs with the least
machinery, reusing the committed + `@`-ref'd surfacing so plans appear in every
session. Rejected B/C per axiom 2; D folded in as the charter doc. Evidence:
sole writer `atomic/internal/followups/add.go`; schema `entry.go`; severity enum
`entry.go`; `kind` is additive.

## Checkpoints

| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | `kind` schema (default `finding` on read), `--kind` flag (`--severity` optional for plan, invalid kind → exit 1), `📋 plans` render section, `kind` in `list --json` — atomic-builder, ~6 files | `atomic/internal/followups/{entry,add,cli,render,list}.go` + tests | missing-kind→finding; `add --kind plan` frontmatter; invalid-kind exit 1; render places plan in plans section with link; existing entries render unchanged; `--json` shows kind |
| 2 | Exempt `kind: plan` from staleness at the `isStale` seam — atomic-builder, ~3 files | `atomic/internal/followups/{render,list}.go` + `templates/commands/follow-up.md` + tests | plan past `review_by` not stale; finding staleness unchanged |
| 3 | Charter `CLAUDE.md` follow-ups entry (two kinds + manual capture); register nothing skill-related — atomic-surgeon, 1 file | `CLAUDE.md` | bullet names both kinds + `--kind plan` capture |
| 4 | Regenerate render + bundle + refresh signals — atomic-surgeon | `commands/`, `atomic/internal/embedded/**`, `.claude/project/signals*` | render + bundle parity clean; `atomic doctor` no new WARN/FAIL |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| `kind` field breaks parsing of existing entries | med | Default missing `kind` → `finding` on read; test asserts existing entries render unchanged |
| Render change regresses existing severity groups | med | Test pins the full INDEX render against fixtures with mixed kinds |
| Forgotten staleness consumer still nags plans | low | Exemption at the single `isStale` seam; tests cover render count, `list --stale`, review flow |

## Implementation log

### v1 — 2026-05-30

Shipped within issue #28 (commits squashed into one `feat(followups)` change).
Scope: `kind` field (finding|plan), `📋 plans` render, staleness exemption,
`kind` in `list --json`, `CLAUDE.md` charter.

**Key decision:** an auto-firing `atomic-defer` capture skill was built during
the loop, then removed before ship — it had been carried over from the original
issue framing without re-confirmation after the design pivoted to typing the
ledger. Capture is manual via `atomic followups add --kind plan`. Recorded so the
reversal is visible; see the design doc for the reasoning.

**Out-of-scope work:** none.

**Deferred items still open:** none.

## Change log

<!-- Empty. First entry on the first amendment after this spec ships. -->
