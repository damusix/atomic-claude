---
description: Pre-design evidence gathering. Takes a hunch or hypothesis and chases it through primary sources (context7, official docs, source code, ast-grep, run-it experiments) before any spec is written. Returns SUPPORTED / UNSUPPORTED / MIXED / INCONCLUSIVE with cited evidence trail. Pairs with /pressure-test and precedes /atomic-plan.
argument-hint: "[<hypothesis-phrase> | @<path-to.md>]"
---

# /gather-evidence

Pre-design verification. The user has a hunch — *"X library supports Y", "our codebase already has a Z pattern we can reuse", "approach A is faster than B"* — and wants evidence before sinking a planning session into it. This command does that gathering and returns a structured verdict.

Not a code reviewer (`atomic-reviewer`). Not a strategist (`atomic-strategist`). Not a debugger (`atomic-debug` skill). Not a completion check (`atomic-verify`). One job: **gather evidence on a hypothesis before it becomes a spec.**

## Workflow position

```
hunch → /gather-evidence → /pressure-test → /atomic-plan → /subagent-implementation
        (does it hold?)    (is design right?)  (write spec)    (build it)
```

`/gather-evidence` is the first gate. Don't sink a planning session into a hunch that disintegrates on contact with reality.

## Parse arguments

`$ARGUMENTS` may be empty, a hypothesis phrase, or a path to a markdown file.

1. Token starts with `@` or ends in `.md` (resolved relative to cwd, absolute allowed, must exist) → **target document**. Read it. Treat the document's central claims as the hypothesis set.
2. Anything else → **hypothesis seed** prose. Use it as-is.
3. Empty `$ARGUMENTS` → scrape the recent conversation for the most recent unverified factual claim the **user** made (not your own assertions). If multiple candidates exist, pick the most recent one and ask the user to confirm or substitute before continuing. If none exists, ask: *"What hypothesis do you want me to gather evidence on?"*

**Path safety.** Resolve absolute path. If the file doesn't exist or isn't markdown, print one line: `path '<x>' not found (or not markdown) — pass a hypothesis phrase instead.` and stop. If the path resolves outside `git rev-parse --show-toplevel`, print: `path '<x>' outside repo — continuing without target.` and stop.

<workflow>

## Four-step skeleton

### 1. RESTATE

Restate the hypothesis as a **falsifiable claim**. One sentence. Name what would have to be true.

If the hypothesis is vague ("this would be cleaner", "X is better than Y", "we should use Z"):

- **One sharpen round.** Ask: *"What specific behavior, fact, or capability are you betting on? Frame so it can be checked."*
- If the sharpened version is falsifiable → continue to SCOPE.
- If it still can't be made falsifiable → exit with: `hypothesis is unfalsifiable — this is /pressure-test territory, not /gather-evidence. Try running /pressure-test instead.`

Refusals (exit immediately, name the reason):

- Requires credentials or access not available in-session → `cannot gather — needs <X>. Provide or run after acquiring access.`
- Predicts future state ("will this scale", "will users like this") → `predictive claims can't be gathered, only reasoned. Try /pressure-test or build a small probe.`

### 2. SCOPE

Name the bar **before** gathering. One or two lines: *"This is settled if context7 confirms X, or if `grep -r Y src/` returns zero matches. Anything past that is overkill."*

Without a scope, evidence-gathering runs forever. With one, you know when to stop.

State the scope in the report header so the user can challenge it before you commit time.

### 3. GATHER

Pull evidence **both for and against** the hypothesis. Confirmation bias is the failure mode this step prevents — actively look for what would disprove the claim, not only what supports it.

**Source-quality tiers — every evidence bullet must cite source + tier:**

| Tier | Sources | Treatment |
|------|---------|-----------|
| 1 — primary | Official docs, `context7` MCP, source code itself, RFCs / standards, official changelogs, ast-grep on actual repo, `atomic code explore "<query>"` / `atomic code callers <symbol>` / `atomic code impact <symbol>` (when a code-intel index is present) | Default. Always try first. |
| 2 — authoritative | Maintainer-authored issues / PRs / forum posts, official conference talks, peer-reviewed papers | Cite freely. |
| 3 — community signal | Stack Overflow (high-vote + recent, ideally accepted answer), official-adjacent wikis | Cite, weight less. |
| 4 — opinion | Personal blogs, Medium, Reddit, HN, tutorials | Use only when nothing above exists. Flag explicitly as low-trust. |

**Tool routing — judgment, not table-lookup:**

- **Library / framework / API claims** → `context7` first (`resolve-library-id` → `query-docs`). Then `WebFetch` against the official docs URL if context7 misses or the library isn't indexed.
- **Codebase claims** ("function X is called from N places", "we already have a Z pattern", "changing X affects Y") → when a code-intel index is present: for an open-ended claim where the symbol name is unknown ("do we already have pattern Z"), lead with `atomic code explore "<natural-language query>"` — one shot returns the relevant symbols, files, and relationships, scoping the surface before you commit to a targeted query. Once you have the symbol, `atomic code callers <symbol>` answers "called from N places" and `atomic code impact <symbol>` answers "changing X affects Y" directly and authoritatively (Tier 1 — real graph edges, not filename guesses); fall back to `ast-grep` (`sg run -p '<pattern>' -l <lang>`) when the index is absent. Use `grep`/`Grep` for literal strings, log messages, config values regardless. **Note:** `/gather-evidence` may query `atomic code` inline — it owns its evidence-gathering tokens. This is the documented exception to the parents-delegate rule (commands that run in the main context and dispatch subagents for graph queries); evidence gathering is single-threaded by design (behavioral rule 5) and the token cost here is intentional.
- **Behavioral claims** ("does the current code return Y for input X") → write a script to `tmp/`, run it, capture output. Real execution beats reasoning about behavior.
- **CLI / tool claims** → `--help`, `man`, then official docs.
- **Historical / git claims** ("when was X introduced", "was Y ever removed") → `git log -p`, `git show`, `git blame`.
- **External service / API behavior** → `curl` with `-s -o /dev/null -w '%{http_code}'`, verify status and response shape.
- **Performance claims** → benchmark in `tmp/`. Real numbers, not estimates.
- **General factual claims with no library or codebase target** → `WebSearch` last resort. Prefer official-domain results. If only Tier-3 or Tier-4 hits surface, say so.

**WebSearch discipline.** Prefer official domains (the project's own site, the org's GitHub, the standard body's site). Skip personal blogs, listicles, content-farm SEO results unless they're the only signal — and flag them as Tier 4 when used.

**Evidence collection rules:**

- Every check prints the exact command run and the relevant output excerpt. No *"I checked and it's fine."*
- Triangulate high-stakes claims — at least two independent sources where the call commits real time downstream.
- If a hypothesis has multiple sub-claims, gather evidence on each separately. Don't fold them into one verdict.
- Capture counter-evidence with the same rigor as supporting evidence.

### 4. REPORT

Structured output. Atomic style — terse, tables and bullets, no preamble:

```
HYPOTHESIS: <sharpened, falsifiable form>
SCOPE: <what would settle this>

EVIDENCE FOR:
  - <claim>. [Tier 1: <source/url/command>]
  - <claim>. [Tier 1: <source>]

EVIDENCE AGAINST:
  - <claim>. [Tier 2: <source>]

GAPS:
  - <what we couldn't settle this session and why>

VERDICT: SUPPORTED | UNSUPPORTED | MIXED | INCONCLUSIVE
RECOMMENDATION: proceed to /atomic-plan | abandon | refine hypothesis | dig deeper
```

**Verdict definitions:**

- **`SUPPORTED`** — primary-source evidence (Tier 1 or 2) confirms the hypothesis, no significant counter-evidence surfaced within scope.
- **`UNSUPPORTED`** — primary-source evidence directly contradicts the hypothesis. Surface what the truth actually is.
- **`MIXED`** — meaningful evidence on both sides. Recommend refining the hypothesis (often the scope was too broad).
- **`INCONCLUSIVE`** — couldn't settle within scope. Name what would settle it (run CI, ask user for credentials, wait for a deploy, write a probe).

**Verdict gate — Tier rule:** if supporting evidence is only Tier 3 or Tier 4, the verdict cannot be `SUPPORTED`. Caps at `MIXED` or `INCONCLUSIVE` with explicit note: *"supporting evidence is community-level only; primary sources unavailable or silent."* Hearsay does not get to be proof.

**Recommendation definitions:**

- **`proceed to /atomic-plan`** — hypothesis holds, design phase can start.
- **`abandon`** — hypothesis is wrong, do not invest planning time.
- **`refine hypothesis`** — the hunch points at something real but the framing is off. Suggest a sharper version.
- **`dig deeper`** — INCONCLUSIVE with a clear next probe. Name the probe.

</workflow>

## Behavioral rules

1. **Both-sides evidence is mandatory.** Pure confirmation reports are a failure mode. If the GATHER step produced only supporting bullets, run one more pass looking for counter-evidence before reporting. If the second pass also returns nothing, note explicitly in GAPS: *"no counter-evidence surfaced after a directed search."* That is a meaningful signal, not a free pass to `SUPPORTED` — apply the tier rule first.
2. **Cite or omit.** No evidence bullet without a source and tier. *"It's well-known that..."* is not evidence.
3. **One sharpen round, then exit.** Don't burn a session sharpening a hypothesis that won't sharpen. Hand off to `/pressure-test`.
4. **No artifacts beyond the report.** No spec, no design, no code. If the user asks for those, suggest `/atomic-plan` after the verdict lands.
5. **No subagent dispatch in v1.** Single-thread gathering. Re-evaluate if multi-claim audits become a real pattern.
6. **Stop at SCOPE.** Once the bar from step 2 is hit, write the report. Don't keep digging because *more evidence would be nice*.
7. **Verify before asserting.** Same rule as `<investigate_before_answering>` in `CLAUDE.md`. Every factual statement in the report must trace to a tool call output captured in the GATHER step.

## What this command does not do

- Does not write durable artifacts. The report is the artifact; it lives in the conversation.
- Does not save memory. Specific recurring assumptions can be saved manually via the standard memory mechanism — not auto-hooked here.
- Does not dispatch subagents in v1. Single-thread evidence gathering.
- Does not auto-fire. Explicit invocation only.

## When to suggest the next step

After the report:

- `SUPPORTED` + `proceed to /atomic-plan` → one-line hint: *"hypothesis holds — `/atomic-plan` next to capture into a spec."*
- `SUPPORTED` but design tradeoffs surfaced → *"hypothesis holds, but tradeoffs noted above. `/pressure-test` first if you want to challenge the design before planning."*
- `MIXED` / `refine hypothesis` → suggest the sharper version and offer to re-run `/gather-evidence` on it.
- `UNSUPPORTED` → *"hypothesis disconfirmed — primary sources point a different direction. Try `/pressure-test` to find what's actually worth designing around, or restate the hunch and re-run."*
- `INCONCLUSIVE` → name the probe that would settle it. Often a small `tmp/` experiment or a manual check the user can do offline.

Surface as a one-line hint, not a directive. The user chooses.
