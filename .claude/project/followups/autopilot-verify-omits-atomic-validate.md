---
id: autopilot-verify-omits-atomic-validate
title: Verify gate omits atomic validate (autopilot shipped S5-failing spec)
created: "2026-06-06"
origin: |
    PR #38 / watch-ci diagnosis 2026-06-06
kind: finding
severity: risk
review_by: "2026-08-05"
status: open
file: commands/autopilot.md
---

PR #38 (project-wiki) went red in CI on `atomic validate spec` rule S5: `docs/spec/wiki.md` shipped a 6-column `## Checkpoints` table (`# | Checkpoint | Files/areas | Agent | Est. | Verifies`) where S5 requires the canonical 4-column header `| # | Checkpoint | Files/areas | Verifies |`. Fixed in 20870af by collapsing the table.

Two compounding causes let the malformed spec ship "verified":

1. **The verify gate never runs `atomic validate`.** Confirmed: neither `commands/autopilot.md` (Phase 4 Verify), `commands/subagent-implementation.md` (Phase 3 finalize), nor `skills/atomic-verify/SKILL.md` invoke it. atomic-verify checks tests / build / lint / typecheck only. So a structurally-invalid spec passes local verification and fails only in CI — the repo's own validator is not in the pre-ship gate.

2. **The /atomic-plan template emits 6 columns.** `templates/commands/atomic-plan.md:157` emits `| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |`, which S5 rejects. All 28 other specs are 4-column (authors trim by hand); the autopilot-authored wiki spec followed the template verbatim and slipped through because of cause 1.

Fix options:

- **(higher leverage)** Add `atomic validate` to the verify gate — autopilot Phase 4, the atomic-verify skill, subagent-implementation Phase 3. Catches every validator rule before ship, not just S5. Edits: `templates/commands/autopilot.md`, `skills/atomic-verify/SKILL.md`, `templates/commands/subagent-implementation.md` (bundle sources → make render + bundle).
- Reconcile `templates/commands/atomic-plan.md:157` to the 4-column header so template-authored specs pass S5 by default (bundle source → make render + bundle).

Either alone closes this gap; both is belt-and-suspenders. Cause 1 is the root fix — it would have caught this (and any future S-rule break) before it ever reached CI.
