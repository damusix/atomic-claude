# Evidence method

Take a hypothesis phrase or an `@`-prefixed Markdown path. Resolve paths within the repository and read the target; reject missing, non-Markdown, or outside-repository paths. With no argument, propose the user's most recent unverified claim for confirmation, or ask for a hypothesis.

1. Restate each claim in falsifiable terms. Ask one clarifying question if needed; if it remains subjective or predictive, suggest pressure-testing or a bounded probe instead.
2. State what evidence would settle each claim and the scope of the search.
3. Seek evidence for and against it. Prefer source code, official documentation, standards, and direct observations; use maintainer or peer-reviewed sources next, and label community or opinion sources as lower trust. Check high-stakes claims against independent sources. Record exact commands and relevant output for executed checks.
4. Report the hypothesis, scope, evidence for and against with sources and quality, gaps, a verdict, and a recommended next action. Separate subclaims. Stop when the stated scope is met.

Verdicts: **SUPPORTED** requires authoritative evidence with no significant counter-evidence in scope; **UNSUPPORTED** requires direct contradiction; **MIXED** means meaningful evidence on both sides; **INCONCLUSIVE** means the scope did not settle the claim. Community or opinion evidence alone cannot establish SUPPORTED. If a directed search finds no counter-evidence, say so. Name inaccessible sources and the next check that would close a gap. Do not write a design, spec, or code as part of this method.
