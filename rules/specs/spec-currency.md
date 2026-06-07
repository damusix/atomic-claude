---
paths:
  - "docs/spec/**/*.md"
  - "docs/design/**/*.md"
---

# Specs: the body is current truth, the change log is history

`docs/spec/<topic>.md` is a contract read by fresh-context subagents as ground truth. **The body must always describe the *current* decision — never superseded content.** A subagent reads the body verbatim and builds what it says; if the body still describes work a later decision cut or changed, the subagent builds the wrong thing. Preventing that is the entire point of this rule.

**Why:** the body and the change log have different jobs. The body says what is true *now*. The log says *how it got here*. Conflating them — leaving old behavior in the body "for the record" — turns the contract into a hallucination source.

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
