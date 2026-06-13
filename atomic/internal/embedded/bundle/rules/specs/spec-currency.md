---
paths:
  - "docs/spec/**/*.md"
  - "docs/design/**/*.md"
---

# Specs: the body is current truth, the change log is history

`docs/spec/<topic>.md` is a contract read by fresh-context subagents as ground truth. **The body must always describe the *current* decision — never superseded content.** A subagent reads the body verbatim and builds what it says; if the body still describes work a later decision cut or changed, the subagent builds the wrong thing. Preventing that is the entire point of this rule.

**Why:** the body and the change log have different jobs. The body says what is true *now*. The log says *how it got here*. Conflating them — leaving old behavior in the body "for the record" — turns the contract into a hallucination source.

## The body is forward-only

This holds for a *fresh* spec as much as an amended one. The body describes only what is going to be built — Goal, Non-goals, Success criteria, Checkpoints, Risks. It never narrates how the plan got there. Three things belong in the design doc, never the spec body:

- **Decision history** — "resolved during the pressure-test", "we decided that…", "after discussion". The resolution is already encoded as a success criterion or a non-goal; recounting *how* it was reached is design/audit material.
- **Prior-version references** — "previously", "superseded", "was going to", "the earlier draft". A fresh reader has no earlier draft to compare against; the body is the only version they see.
- **Rejected-alternative enumeration, even as a pointer** — naming which options were weighed leaks the deliberation. When a design doc exists the spec carries a one-line `## Approach` pointer that names only the chosen option and links the design; it does not list the forks.

Negative scope in `## Non-goals` is fine — "no `--db` flag" is a current-truth constraint on what to build, not history. The test: a sentence that only makes sense to someone who watched the plan evolve does not belong in the body.

## Amendment rules

Every spec ends with a `## Change log` section. Append a dated entry per amendment; never delete prior entries. The log is the audit trail, but it never substitutes for keeping the body current.

- **Adding behavior** → new body section + log entry (`### YYYY-MM-DD — <title>` + **What changed** + **Why**).
- **Changing / superseding behavior** → **rewrite the affected body sections to the new truth**, then log it with a **Superseded:** line summarizing the prior contract. Do not leave the old behavior described anywhere in the body.
- **Removing behavior** → delete it from the body + log entry with a **Removed:** line and reason. A rejected *approach* moves to the design doc's rejected-approaches section, not a lingering spec body.
- **Spec was wrong** → correct the body in place + log entry prefixed **Correction:** with how you know (test failure, prod incident, code diverged) and the truth.
- **Renaming / splitting** → final log entry on the old file pointing to the new location. Keep the old file one commit longer so grep finds both.

## Change-log entry template

```markdown
### YYYY-MM-DD — <short title>

**What changed:** <one paragraph>

**Why:** <trigger — bug, feedback, axiom, dependency>

**Superseded:** <if applicable, one line on the prior contract>
```

The test before handing a spec to a subagent: could a fresh reader, reading only the body, build something the latest decision already cut? If yes, the body isn't done. A long change log is healthy; a body that contradicts the latest decision is a defect.
