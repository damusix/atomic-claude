---
id: wiki-fingerprint-legacy-signals-path
title: Indexed-member fingerprint probes pre-relocation signals.md, silently falls back to HEAD
created: "2026-08-17"
origin: |
    docs concision pass, signals->wiki rename review
kind: finding
severity: risk
review_by: "2026-10-16"
status: open
file: atomic/internal/wiki/stamp.go:72
---

`resolveFingerprint` resolves an indexed member's content fingerprint by reading
`<repo>/.claude/project/signals.md` — the pre-relocation path. After the
signals-to-wiki relocation an indexed member's router lives at
`<repo>/docs/wiki/index.md`, so on any migrated repo that `os.ReadFile` always
misses and the function falls through to `gitRevParseHead(repoDir)`.

Effect: concern `reflects:` fingerprints for indexed members key on the member's
HEAD SHA instead of its wiki content. Any commit to that member marks every
concern citing it STALE, even a commit that never touched the wiki — the exact
"nudge becomes noise" failure `atomic wiki stale` is designed to avoid. The
inverse also holds: editing the member's wiki without committing does not
change the fingerprint.

Contrast with `atomic/internal/wiki/wiki.go:320-333`, which classifies members
by checking `docs/wiki/index.md` FIRST and only then falling back to the legacy
path. `resolveFingerprint` never gained the same new-path probe.

Fix: probe `docs/wiki/index.md` before `.claude/project/signals.md` in
`resolveFingerprint`, mirroring the classification order. Legitimate legacy-path
uses in `cmd_migrate.go:159` and `migrate/steps.go:26` are migration detection
and must stay.

Documented as a known gap in `docs/reference/realm-wiki.md` (staleness section);
remove that warning block when this is fixed.
