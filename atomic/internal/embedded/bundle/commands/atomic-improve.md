---
description: Session retrospective. Mines `.jsonl` history and this conversation for friction, corrections, and misbehavior; cross-references installed artifacts; walks findings one at a time. Persists a run log so later runs detect drift. Use after long sessions or repeated friction.
---

You orchestrate a retrospective audit. Subagents do the scanning (read-only). You categorize, present findings indexed, and apply only what the user accepts per item.

`$ARGUMENTS` is targeted feedback — any free-form hint ("the tool-use felt off today", "audit my skills budget") gets highest priority but never narrows scope. The audit is always a full sweep.

<workflow>

## Pre-flight

1. Announce: `Starting retrospective.`
2. Resolve state paths once and cache them:
    ```
    ATOMIC_STATE="${HOME}/.claude/.atomic"
    RUNS_DIR="${ATOMIC_STATE}/improve-runs"
    LEARNINGS="${ATOMIC_STATE}/improve-learnings.md"
    SCRATCH=".claude/.scratchpad/$(date +%Y-%m-%d)-improve"
    mkdir -p "$RUNS_DIR" "$SCRATCH"
    RUN_ID="$(date +%Y-%m-%d-%H%M%S)"
    ```
3. Resolve the current Claude project session dir (used by history scan):
    ```
    PROJECT_SLUG=$(pwd | sed 's|/|-|g')
    SESSIONS_DIR="${HOME}/.claude/projects/${PROJECT_SLUG}"
    ```
    If `$SESSIONS_DIR` does not exist, the history scan will degrade to current-only — note that in the run summary, do not abort.
4. Read `$LEARNINGS` if it exists. It carries: acceptance rates per category, modify-signal patterns, deprioritized finding types. Apply as soft weights during Phase 4 categorization. If absent, proceed — it will be created at the end of the run.

## Step 1 — Pick scope

Prompt via `AskUserQuestion`:

```
Question: What scope should this retrospective cover?
Options:
  - Historical + current conversation (recommended) — last 5 .jsonl sessions + prior /atomic-improve audits + this conversation
  - Current conversation only — skip history scan, no prior-audit cross-check
```

Store the answer as `$SCOPE`.

## Step 2 — Dispatch background agents in parallel

Single message, two or three `Agent` tool calls. Briefs live in `$SCRATCH/`.

### 2a. Discovery — `atomic-investigator` (always)

Write `$SCRATCH/discovery-brief.md`:

```markdown
# Discovery brief

Catalog every config-shaped file at BOTH installed (~/.claude) and project levels. Return a `file:line — purpose` table grouped by kind and level.

Look for:

- Global: ~/.claude/CLAUDE.md, ~/.claude/CLAUDE.local.md, ~/.claude/commands/, ~/.claude/agents/, ~/.claude/skills/, ~/.claude/output-styles/, ~/.claude/rules/, ~/.claude/settings.json, ~/.claude/settings.local.json, ~/.claude/.atomic/config.toml, ~/.claude/.atomic/config.resolved.md, ~/.claude/.atomic/profile.md
- Project: ./CLAUDE.md, ./CLAUDE.local.md, .claude/commands/, .claude/agents/, .claude/skills/, .claude/settings.json, .claude/settings.local.json, .claude/project/signals.md, .claude/project/followups/INDEX.md, .claude/project/followups/*.md
- Memory: ~/.claude/projects/${PROJECT_SLUG}/memory/MEMORY.md and topic files

For each artifact: path, one-line purpose, char count, line count.
Do NOT propose changes. Inventory only.

Respond in atomic style. Drop filler, pleasantries, hedging. Fragments OK. Technical terms exact. Inventory table only — no preamble, no echo of this brief.
```

Dispatch with `subagent_type: "atomic-investigator"`. Prompt: `Read $SCRATCH/discovery-brief.md and return the inventory.`

### 2b. History scan — `atomic-haiku` (full scope only)

Skip if `$SCOPE = "current-only"`.

Write `$SCRATCH/history-brief.md`:

```markdown
# History scan brief

Scan the 5 most recently modified `.jsonl` session files in `${SESSIONS_DIR}` (exclude the current session, identified by mtime within the last 60 minutes if no other heuristic).

For each session, extract ONLY user-typed messages. Each row in a `.jsonl` is a single JSON object; the shape varies but user messages look roughly like:

```json
{"type":"user","message":{"role":"user","content":"actual text the user typed"},"timestamp":"2026-05-20T14:32:08Z","sessionId":"…"}
```

Or with structured content:

```json
{"type":"user","message":{"role":"user","content":[{"type":"text","text":"actual text"}]}}
```

Skip rows where `message.content` is an array containing `tool_result` blocks — those are not user input, they are tool outputs threaded as user-role messages. Also skip rows where the message is empty or whitespace-only.

Filter the extracted text for:

- Corrections: "no", "don't", "stop", "not that", "wrong", "actually", "instead"
- Praise: "yes", "perfect", "exactly", "great", "love"
- Explicit feedback: "improve", "better", "should", "I wish", "next time"
- Frustration: "again", "I already said", repeated identical requests

**Atomic-meta detection (positional, not name-matching).** Do NOT search for literal mentions of atomic skill/agent/command names — users rarely complain in atomic's vocabulary. Instead:

1. Identify rows where an atomic artifact was active in the preceding ~5 turns. Signals: an `assistant` row with a `tool_use` whose name matches `atomic-*` (subagent dispatch), or any user/assistant row mentioning an atomic command (`/commit-only`, `/atomic-plan`, etc.) or skill (anything under `~/.claude/skills/atomic-*` or invoked via the `Skill` tool with an atomic skill name).
2. For each such window, look for frustration / correction signals in the *next* user message (within 5 turns). The frustration anchors on what came before in the conversation, not on naming the artifact.
3. If a correction or frustration signal lands in that window, mark `atomic_meta = true` and capture the active atomic artifact name in `meta_target`.

**Profile drift detection.** Read `~/.claude/.atomic/profile.md` if it exists (skip silently if absent). Parse facts from `<stable>` and `<volatile>` sections (skip `<deterministic>` — never flagged). For each user-typed message in the session, scan for statements that contradict or supersede an existing fact. Examples:
- profile says `Employer: Acme`; user writes "at Globex we did it this way" → drift candidate.
- profile says `Role: Senior eng`; user writes "now that I'm a staff engineer" → drift candidate.

For each drift candidate, return a finding with:
- `category = "profile drift"`
- `existing_fact` (the line from profile.md, verbatim) → stored in `meta_target` column
- `new_fact` (the user's contradicting statement) → stored in `quote` column
- `confidence` (`low` / `medium` / `high` based on contradiction strength) → stored in `recurrence_across_sessions` column as `confidence:<level>`
- `session_date`

`<deterministic>` section facts are excluded — Claude does not write to those sections and they should never drift.

Return a table:
| session_date | category | quote (≤120 chars; for profile-drift: new_fact) | recurrence_across_sessions (for profile-drift: confidence:<low\|medium\|high>) | atomic_meta (bool) | meta_target (for profile-drift: existing_fact verbatim) |

For profile drift rows: `quote` = new_fact, `meta_target` = existing_fact (verbatim from profile.md), `recurrence_across_sessions` = `confidence:<low|medium|high>`, `atomic_meta` = false.

Mark recurring patterns (same complaint in 2+ sessions). No raw transcripts. Read-only.

Respond in atomic style. Drop filler, pleasantries, hedging. Fragments OK. Findings only — no preamble, no echo of this brief.
```

Dispatch with `subagent_type: "atomic-haiku"`. Prompt: `Read $SCRATCH/history-brief.md and execute. Read-only.`

### 2c. Prior-improve audit — `atomic-haiku` (full scope only, and only if `$RUNS_DIR` has entries)

Skip if no prior runs exist.

Write `$SCRATCH/prior-improve-brief.md`:

```markdown
# Prior-improve audit brief

For each `<ts>.json` in `${RUNS_DIR}` (most recent 10), parse the `decisions[]` array. For every decision where `disposition == "accept"`:

- Read `target_file` from the decision.
- If `verify_absence == true`: landed if the file is absent; drifted if it exists.
- Otherwise: if the file does not exist → mark `missing`; if it exists, grep for `verify_phrase`. Present → `landed`. Absent → `drifted`.

For every `disposition == "skip"` OR `disposition == "suppressed"`, capture the `signal_keywords` so the current run can detect recurrence (same complaint, same files, similar quote) and re-surface the item at tier 2. Suppressed and skipped re-surface by the same mechanism — the user never had the chance to actively decline a suppressed item, so it should not require stronger recurrence signal than a skipped item.

`disposition == "routed-to-issue"` does not re-surface — the user has already taken action by filing an issue.

Return:
| run_ts | total_accepts | landed | drifted | missing | skipped | suppressed | recurring |

Plus a separate list of `drifted` and `missing` items for re-surfacing (with target file and verify phrase), and a list of `skipped` + `suppressed` items whose `signal_keywords` reappear in the current run.

Respond in atomic style. Drop filler, pleasantries, hedging. Fragments OK. Audit table + re-surface list only — no preamble, no echo of this brief.
```

Dispatch with `subagent_type: "atomic-haiku"`. Prompt: `Read $SCRATCH/prior-improve-brief.md.`

## Step 3 — Analyze current conversation (foreground, while agents run)

Scan the in-context conversation for the same signal categories the history scan uses, plus:

| Signal | What to look for |
|--------|------------------|
| Corrections in this session | Direct user pushback, redirects, "no, do X instead" |
| Praise in this session | "yes", "exactly", "keep doing that" — quieter than corrections, easy to miss |
| Friction loops | Same task attempted 2+ times, confusion about output, re-asks |
| Capability gaps | User did something manually that a skill/command could have done |
| Behavioral patterns | Over-explaining, missing context, wrong tool choice, ignored axioms |
| Techniques discovered | Novel approaches that worked — candidate for codification |
| Targeted feedback | `$ARGUMENTS` — flagged HIGHEST priority |
| Atomic-meta frustration | Frustration or correction landing within ~5 turns *after* an atomic artifact was active (subagent dispatch, atomic skill invocation, atomic slash-command run). The user rarely names the artifact in their complaint — the position in the conversation does. Look back from each correction/frustration signal: was an atomic-* artifact the most recent acting party? If yes, it's atomic-meta. Capture the artifact name as `meta_target`. |

Capture findings in `$SCRATCH/current-conv-findings.md` as `category | quote | proposed_action | atomic_meta(bool) | meta_target`.

## Step 4 — Wait for agents, reflect, fallback on failure

After each agent returns, reflect on the result before proceeding. Malformed output — missing table headers, body where data was expected, hedged narrative instead of structured findings — is a failure even if the agent didn't error. Treat as empty and apply fallback. Do not let garbage data pollute Phase 5.

Then for each agent:

| Agent | If empty / failed | Action |
|-------|-------------------|--------|
| Discovery | Empty | Inline fallback: `ls` the hardcoded path list from 2a, build the inventory yourself. Announce: `Discovery agent returned empty — using inline fallback.` |
| History scan | Empty or no sessions found | Note in run summary, skip Phase 4b pattern promotion. Announce: `History scan skipped — no session files found.` |
| Prior-improve | No prior runs | Skip prior-improve audit display. No announcement needed (first run is expected). |

Never silently proceed with missing data — surface what was skipped and why.

## Step 5 — Cross-reference and categorize

<thinking_guidance>

Categorization decides what becomes a finding at all — get this wrong and the user walks noise. Anchor every tier assignment in observable signals, not model inference:

- A finding is **Critical** only when there is recurrence (≥2 sessions OR ≥2 violations in current conversation) OR a direct user pushback. Never assign Critical from a single inferred pattern.
- A finding is **Atomic-meta** only when the frustration is positionally co-located with an atomic artifact (see history brief). Vague "this is frustrating" with no atomic artifact in the preceding window is just **User coaching** or no finding at all.
- Weight direct user corrections > recurring history pattern > single-session signal > model-inferred pattern. The last category caps at confidence: low.
- When two signals point at the same target file with different recommendations, surface as ONE finding with the conflict named, not two competing ones.
- If the only evidence is "the artifact looks unusual to me," that's not a finding — drop it.

</thinking_guidance>

Read each artifact from the discovery inventory (only the files that current-conv or history signals point at — do not re-read everything).

### 5pre. Delegate deterministic audits to the binary

Several Phase 5 checks reimplement what the `atomic` binary already does deterministically. Per axiom 2 — prefer code over model for deterministic transforms. Run these first, parse JSON, classify; only fall through to LLM judgment for items the binary cannot decide.

```bash
# Project integrity — ref wiring, manifest, follow-ups, memory, binary version
atomic doctor --json --skip signals,binary 2>/dev/null > "$SCRATCH/doctor.json" || true

# Artifact lint — spec markdown structure, cross-reference integrity, bundle parity
atomic validate --json 2>/dev/null > "$SCRATCH/validate.json" || true

# Doc surface staleness
if ! atomic docs stale 2>/dev/null; then
  echo "stale" > "$SCRATCH/docs-stale.txt"
fi

# Signals staleness — newest source artifact vs signals.md mtime
SIGNALS_MTIME=$(stat -f %m .claude/project/signals.md 2>/dev/null || echo 0)
NEWEST_SOURCE=$(find agents commands skills CLAUDE.md -type f -newer .claude/project/signals.md 2>/dev/null | head -1)
[ -n "$NEWEST_SOURCE" ] && echo "$NEWEST_SOURCE" > "$SCRATCH/signals-stale.txt"
```

Parse the outputs:

- `doctor.json` — every `FAIL` becomes a candidate finding (tier: Critical if `category in {refs, manifest, install, hooks}`; Maintenance otherwise). Every `WARN` becomes a Maintenance finding. SKIP/PASS are ignored.
- `validate.json` — every `FAIL` with `level >= S1` becomes a Critical finding; lower levels become Maintenance.
- `docs-stale.txt` exists → one Maintenance finding suggesting `/documentation` re-scan.
- `signals-stale.txt` exists → one Maintenance finding suggesting `/refresh-signals`.

These findings carry the binary's own remediation text — the LLM does not paraphrase it. Recommendations are the binary's verbatim `--fix` suggestion when present.

If `atomic` is not on `PATH` (binary not installed in this project), skip this step entirely and announce `atomic binary absent — deterministic audits skipped, falling through to LLM checks only.` Continue.

### 5a. Enforcement-gap detection → hook proposal

For each rule in `CLAUDE.md` / `~/.claude/CLAUDE.md` / skills:

- Did the current conversation violate it? Single violation → propose **strengthen** (NEVER/ALWAYS prefix, move higher).
- Did history scan show 2+ violations across sessions, OR did current conversation violate it 2+ times? Propose **convert to hook**.

When proposing a hook, generate the full `.claude/settings.json` patch ready to paste. Pick the right event:

| Rule shape | Hook event |
|------------|-----------|
| Forbidden bash flag or command pattern | `PreToolUse` matcher `Bash`, command greps tool_input, exits 2 on match |
| Forbidden tool entirely | `PreToolUse` matcher `<ToolName>`, command exits 2 unconditionally |
| Required follow-up after a tool ran | `PostToolUse` matcher `<ToolName>`, command runs validation |
| Periodic announcement / reminder | `Notification` |

Hook payload template (substitute the rule check):

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {
            "type": "command",
            "command": "jq -r '.tool_input.command' | grep -qE '<PATTERN>' && { echo 'blocked: <REASON>' >&2; exit 2; } || exit 0"
          }
        ]
      }
    ]
  }
}
```

When presented in Phase 6, offer four sub-options: **Strengthen rule** / **Convert to hook (project)** / **Convert to hook (global)** / **Both**. Hook writes are delegated — the user picks scope, then either `/update-config` is invoked or the file is patched directly with confirm.

### 5b. Pattern promotion (full scope only)

Skip if `$SCOPE = "current-only"`. Otherwise, for every signal that recurred across 2+ history sessions:

- Memory entry → CLAUDE.md rule (if behavioral, applies broadly)
- Buried rule → top of CLAUDE.md with NEVER/ALWAYS prefix
- Implicit pattern → explicit rule + concrete example
- Soft guideline ("try to…", "consider…") → hard rule ("always…", "never…")

### 5c. Config health audits (LLM-judgment only)

Step 5pre already covered the deterministic checks (signals staleness, follow-up staleness, ref integrity, manifest, bundle parity, spec structure). This step covers only audits the binary cannot decide — they require LLM reading of artifact *content*, not just structure.

| Audit | Threshold | Finding |
|-------|-----------|---------|
| `CLAUDE.md` size | >100 lines OR >20K chars (warn); >150 lines OR >40K chars (critical) | Suggest skill extraction or rule migration to `.claude/rules/` |
| Memory dir size | >20 files in project memory | Suggest consolidation pass |
| Memory consolidation | Two memory files cover overlapping rules | Propose merge target + grep evidence (mandatory: verify with grep before claiming redundancy) |
| Rule extraction | CLAUDE.md section is path-specific (e.g. "for *.test.ts files") | Suggest move to `.claude/rules/<lang>/<topic>.md` with `paths:` frontmatter |
| Skill extraction | CLAUDE.md section >20 lines reads like a procedure | Suggest skill conversion (loaded on-demand, lower always-on cost) |
| Skill description budget | Total chars across all skill `description` fields >12K (warn), >15K (elevated) | Suggest compression targets, ideal 130 chars per description. Present as **Maintenance** tier always — community-discovered budget, not officially documented. |
| Skill description quality | Description not third-person, missing trigger keywords, <60 or >300 chars | Propose rewrite |
| Cross-skill contradictions | Two skills give conflicting directives or claim same trigger condition | Surface both file:line citations |
| Skill vs CLAUDE.md contradictions | A skill contradicts a rule in `CLAUDE.md` without acknowledging | Critical-tier finding |
| Cross-level duplication | Same rule in project `CLAUDE.md` and global `~/.claude/CLAUDE.md` | Suggest one wins, name which |

### 5d. Content placement audit

Five directions. Each direction produces one finding type:

| Direction | Detect | Propose |
|-----------|--------|---------|
| CLAUDE.md → skill | A CLAUDE.md section references a specific skill, OR describes a workflow only relevant during one skill's execution | Move the section into the skill file |
| Memory → skill | Memory entry of type `feedback`/`project` contains multi-step procedure | Convert to a skill or skill section |
| Skill → CLAUDE.md | Skill contains a universal behavioral rule (applies across all sessions, not just the skill's flow) | Promote to CLAUDE.md |
| CLAUDE.md → memory | CLAUDE.md contains pure facts / external pointers, not behavior | Move to memory (cuts always-on context cost) |
| Skill ↔ skill | Two skills share near-identical sections | Extract to shared reference or CLAUDE.md rule |

### 5e. Categorize all findings

13-tier priority + 3-axis confidence. Categories applied in this order when conflicts arise:

| Tier | When | Priority |
|------|------|----------|
| Drifted | Prior accept didn't land (from prior-improve audit) | 1 |
| Re-surface | Previously skipped finding still signaling | 2 |
| Atomic-meta | User frustrated by atomic-claude itself (command/skill/agent misbehavior). Routes to `/report-issue-with-atomic`. | 3 |
| Targeted | From `$ARGUMENTS` | 4 |
| Critical | Caused errors, repeated correction, enforcement gap with recurrence | 5 |
| Promotion | Recurring cross-session pattern → stronger rule | 6 |
| Content placement | Wrong layer | 7 |
| Improvement | Refinement to existing artifact | 8 |
| Technique | Novel approach worth codifying | 9 |
| Maintenance | Bloat, contradictions, staleness, budget, descriptions | 10 |
| Reinforcement | Worked well — strengthen existing doc | 11 |
| New skill | Repeated pattern → dedicated skill | 12 |
| User coaching | Suggest better prompting interaction | 13 |

Confidence axis (orthogonal to priority):

| Level | Criteria |
|-------|----------|
| High | 3+ supporting signals, OR recurrence across 2+ sessions, OR direct user correction |
| Medium | 1-2 current-session signals, OR pattern match without direct evidence |
| Low | Speculative — inferred from config structure / best practices, no direct user signal |

In `$SCOPE = "current-only"`, cap at Medium unless there is a direct user correction in the current conversation.

### 5f. Placement recommendation (mandatory)

Every finding must include a **single recommended target** with rationale. No equal-weight menus. Lead with the recommendation, present alternatives second.

Decision rule:

- Rule only applies during a specific skill → target that skill file.
- Rule applies across 2+ skills, not universal → CLAUDE.md.
- Universal behavioral rule → CLAUDE.md top section.
- Factual / reference content → memory file.
- Path-scoped instruction → `.claude/rules/<lang>/<topic>.md`.
- Procedure >20 lines → new skill.

## Step 6 — Present findings indexed

Announce: `Found N findings across M tiers. Presenting indexed; review and accept per item.`

### 6a. Audit summary first (full scope only)

If prior-improve agent returned data, print the audit table verbatim:

```
## Prior /atomic-improve audit

| Run        | Accepts | Landed | Drifted | Missing | Skipped | Suppressed | Recurring |
|------------|---------|--------|---------|---------|---------|------------|-----------|
| 2026-05-20 | 6       | 6 ✅    | 0       | 0       | 4       | 12         | 0         |
| 2026-05-12 | 5       | 3 ✅    | 1 ⚠️    | 0       | 2       | 8          | 2         |
```

If any `drifted` or `missing` exists, those rows have already been added to the findings list at priority tier 1. Recurring skips and suppressed items (signal_keywords matched current-run signals) appear at tier 2.

### 6b. Indexed finding list

Print all findings as an indexed list (axiom 4: plain-text indexed selection over multi-select UI when N>4).

**Cap at 15 findings per run.** If total findings > 15, surface only the top 15 by (tier ASC, confidence DESC). Findings 16+ are **suppressed** — persisted to the run log with `disposition: "suppressed"` plus their `signal_keywords`. They re-enter at tier 2 (re-surface) in a future run if the signal recurs, same mechanism as `skip`.

Why 15: at 1-3x/week cadence with ~15min user attention budget, a finding-walk averages ~45sec per item (Accept/Skip fast, Modify slow). 15 × 45sec ≈ 11min walk + apply phase fits the budget. Critical and atomic-meta and drifted always sit at the top of the tier sort, so they're never suppressed. Maintenance noise on a healthy repo gets the suppressed treatment, which is correct — 30 maintenance items in one session is exactly the failure mode the cap prevents.

Note at the bottom of the list when surplus exists:

```
N additional findings suppressed (tiers <list>). They are in the run log and will re-surface in a future run if the signal recurs.
```

<example>

```
[1] atomic-meta · high  | /atomic-plan kept re-asking the same clarify question 3 times this session
                          target: open atomic-claude issue
                          rationale: user said "I already told you the goal" twice; meta-skill misbehavior
                          options: open-issue | skip
[2] critical · high     | repeated --no-verify usage in 3 sessions
                          target: ~/.claude/settings.json (new PreToolUse hook)
                          rationale: rule violated 3× across sessions; advisory text not enforcing
                          options: strengthen | hook-project | hook-global | both | skip
[3] promotion · high    | "always quote file:line in reviews" recurring → CLAUDE.md
                          target: CLAUDE.md §Principles
                          rationale: 2 sessions corrected vague review feedback
                          options: accept | modify | skip
[4] placement · medium  | atomic-tdd skill has a universal rule that belongs in CLAUDE.md
                          target: CLAUDE.md §Quality gates (extract from skills/atomic-tdd/SKILL.md:42)
                          rationale: rule applies outside TDD flows
                          options: accept | modify | skip
```

</example>

Append the selection prompt:

```
---

Type indices to act on. Examples: `1 3 5` | `1,3,5` | `1-3 5` | `all` | `none`
For each selected item, you will be asked per-finding individually (per axiom 3 — destructive ops explicit confirm).

Your selection:
```

Validate indices the same way `/git-cleanup` does. After valid selection, walk each picked finding one at a time via `AskUserQuestion`.

**Standard findings** (most tiers):

```
Question: [N] <one-line summary>
Options:
  - Accept recommendation
  - Modify (provide new wording)
  - Skip (record in run log; re-surfaces automatically if signal recurs in a future run)
```

**Atomic-meta findings** (tier 3 — atomic-claude misbehavior):

```
Question: [N] <one-line summary>
Options:
  - Open issue (routes to /report-issue-with-atomic)
  - Skip
```

On "Open issue", print exactly:

```
Surface this when you're ready to file it:

    /report-issue-with-atomic <one-line summary>

The pre-filled body will need the context (which command/skill/agent, what went wrong, what you expected).
```

Do not auto-invoke the command — per axiom 3, the user runs it themselves. Record `disposition: "routed-to-issue"` in the run log so the prior-improve audit doesn't re-surface it next run.

**Profile-drift findings** (category `profile drift`):

```
Question: [N] "<existing fact>" may be stale — you mentioned "<new observed fact>" in this session.
Confidence: <low|medium|high>
Options:
  - Accept new (record new fact; old fact retained as history per spec)
  - Modify (provide alternative wording)
  - Keep both (no change — both facts coexist; useful when context-dependent)
  - Skip (record in run log; re-surfaces if same drift recurs)
```

On "Accept new": append the new fact to the matching section in `~/.claude/.atomic/profile.md` (below the existing fact, retaining the old line). On "Modify": follow the standard Modify flow (turn-boundary state save). On "Keep both": record `disposition: "keep-both"` in the run log; do not write to profile.md. On "Skip": record `disposition: "skip"` so it re-surfaces.

`<deterministic>` section facts are excluded — they should never appear here.

**Sub-option findings** (e.g. hook conversion in `critical` tier): after Accept, follow up with a second `AskUserQuestion` for the sub-option (strengthen / hook-project / hook-global / both).

**Modify flow** (turn-boundary, not inline read).

`AskUserQuestion` does not collect free-text, and there is no inline-read primitive in the harness. The only way to get the user's replacement wording is to **end the assistant turn** and resume from their next message. The flow:

1. When the user picks `Modify` in the AskUserQuestion for finding `[N]`, persist the pending state to `$SCRATCH/pending-modify.json`:
    ```json
    {
      "finding_index": N,
      "original_recommendation": "<text>",
      "remaining_indices": [<list of unwalked findings>]
    }
    ```
2. Print exactly:
    ```
    Modify [N]: <one-line finding summary>
    Original recommendation: <text>

    Paste your replacement wording in your next message. It will be used verbatim when applying.
    ```
3. End the assistant turn. Do not call further tools.
4. On the next user message, read `$SCRATCH/pending-modify.json`, capture the user's entire message as `decision.modified_content`, delete the pending file, and resume the indexed walk at the next pending finding.

This is a real turn boundary — the apply phase cannot run until every Modify has resolved. If the user instead replies with something off-script (a question, a redirect), abandon the Modify (record as `disposition: "skip"` with a note `"modify abandoned: user redirected"`) and continue the walk from where it stopped.

After every 5 findings walked, offer a checkpoint: `5 findings walked, K remaining. Continue, or apply what we have and skip the rest?` Skip-the-rest records remaining indices as `disposition: "skip"` (not `suppressed` — the user actively chose to stop, the signal is "audit fatigue this session").

## Step 7 — Apply

Announce: `Applying K approved changes across J files.`

For each approved decision, in priority order:

1. Read the target file.
2. Decide the `verify_phrase` **before** writing — patch-unique, not concept-unique. Pick a literal substring of the bytes you are about to write that would not exist if the change were absent. For a hook patch, that's the exact `grep -qE '<PATTERN>' && { echo 'blocked: <REASON>'` slice — not the rule name (`--no-verify`), which exists in many places. For a CLAUDE.md sentence add, the verbatim new sentence. For a memory consolidation, a distinctive phrase from the merged content. The test: would this exact substring exist in the target file *only because the change was applied*? If not, pick a longer slice.

   **Inverted case for deletes.** Memory-file consolidation deletes the source file. There is nothing to grep for. Record `verify_phrase: null` and `verify_absence: true` with `target_file` = the deleted path. Next run's prior-improve audit treats this as: landed if the file is absent, drifted if the file reappeared with similar content.

   **Acceptable side effect.** If the user later edits the change in good faith — softens "NEVER" to "Avoid", reformats the hook with `jq --indent 2` — the next prior-improve audit fires `drifted` on it. That is correct behavior. Drift means "the original change is gone." Whether it was replaced by something better is a separate signal not in scope.
3. For Modify decisions, use the user's text from `$SCRATCH/pending-modify.json` instead of the original recommendation.
4. Apply the change (edit / append / new file / hook patch).
5. **Verify-after-apply.** Re-read the target file (or `ls` for delete cases) and grep for the `verify_phrase` recorded in step 2. If absent (or if a delete target still exists), the write failed silently — set `decision.disposition = "failed"`, capture stderr / the apply error in `decision.failure_reason`, surface a one-line warning to the user (`[N] write failed: <reason> — finding will re-surface in next run`), and continue to the next decision. Do NOT include failed items in the changes-applied table. The verify step is non-negotiable: a silent write failure is worse than a loud one because the user thinks the change landed and the next run's drift audit will report `drifted` ambiguously (as if the change was rolled back, not as if it never landed).
6. For "convert to hook" — see the merge recipe below. Sub-step 4 (Apply) becomes the jq-merge sequence; sub-step 5 (Verify) greps for the hook command-line in the resulting `.hooks.<event>` array.
7. For memory consolidation that deletes files — first integrate content into the target (skill or CLAUDE.md), confirm by re-reading, then delete the source memory file and update `MEMORY.md`. Sub-step 5 (Verify) tests the deleted-path-still-absent condition.
8. For "skip" decisions — no file write. Just persisted in the run log with `signal_keywords` so future runs can detect recurrence. Sub-steps 3-5 are skipped.
9. For "routed-to-issue" decisions (atomic-meta tier) — no file write. The user runs `/report-issue-with-atomic` themselves. Just persisted in the run log. Sub-steps 3-5 are skipped.

### Hook merge recipe

Hook patches must **append** to existing arrays, never overwrite. Use `jq` (preinstalled on most dev machines; if absent, halt with `jq required for hook merge — install via 'brew install jq' or 'apt install jq'`).

For each event key in the patch (`PreToolUse`, `PostToolUse`, etc.):

```bash
TARGET="$HOME/.claude/settings.json"     # or .claude/settings.json for project scope
PATCH='<the generated hook JSON>'
EVENT="PreToolUse"                       # whichever event the patch targets

# Ensure target exists and has a .hooks key
[ -f "$TARGET" ] || echo '{}' > "$TARGET"
jq 'if .hooks then . else .hooks = {} end' "$TARGET" > "$TARGET.tmp" && mv "$TARGET.tmp" "$TARGET"

# Print the resulting .hooks block to the user BEFORE writing
PROPOSED=$(jq --argjson patch "$PATCH" --arg ev "$EVENT" \
  '.hooks[$ev] = ((.hooks[$ev] // []) + ($patch.hooks[$ev] // []))' "$TARGET")
echo "$PROPOSED" | jq .hooks
```

Then confirm with the user via `AskUserQuestion`: `Apply this hook merge to <TARGET>? (yes / no)`. On yes:

```bash
echo "$PROPOSED" > "$TARGET.tmp" && mv "$TARGET.tmp" "$TARGET"
```

Atomic write (write-then-rename) — never edit settings.json in place. **Why:** a half-written settings.json on disk crash kills every future Claude Code session in that scope until repaired.

Print the changes table:

```
## Changes applied

| # | File | Change | Tier |
|---|------|--------|------|
| 1 | ~/.claude/settings.json | added PreToolUse hook blocking --no-verify | critical |
| 2 | CLAUDE.md | added rule under §Principles: "always quote file:line in reviews" | promotion |
| 3 | skills/atomic-tdd/SKILL.md → CLAUDE.md | moved universal rule | placement |

3 changes across 3 files.
```

## Step 8 — Persist run log

Write `${RUNS_DIR}/${RUN_ID}.json`:

```json
{
  "run_ts": "2026-05-27T14:32:08Z",
  "scope": "historical+current",
  "arguments": "<value of $ARGUMENTS or empty>",
  "totals": { "findings": 27, "surfaced": 15, "suppressed": 12, "accepted": 3, "modified": 1, "routed_to_issue": 1, "skipped": 9, "failed": 1 },
  "decisions": [
    {
      "index": 1,
      "tier": "critical",
      "confidence": "high",
      "summary": "repeated --no-verify usage",
      "signal_keywords": ["--no-verify", "skip hooks", "bypass"],
      "target_file": "~/.claude/settings.json",
      "verify_phrase": "grep -qE '--no-verify' && { echo 'blocked: --no-verify forbidden'",
      "verify_absence": false,
      "disposition": "accept",
      "modified_content": null,
      "meta_target": null
    },
    {
      "index": 16,
      "tier": "maintenance",
      "confidence": "low",
      "summary": "skill description below 130 chars",
      "signal_keywords": ["skill description", "activation", "atomic-tdd"],
      "target_file": "skills/atomic-tdd/SKILL.md",
      "verify_phrase": null,
      "verify_absence": false,
      "disposition": "suppressed",
      "modified_content": null,
      "meta_target": null
    }
  ]
}
```

## Step 9 — Update learnings file

Read or create `$LEARNINGS` (the file is `~/.claude/.atomic/improve-learnings.md`).

Append under `## Recent runs`:

```markdown
### 2026-05-27 — run-id 2026-05-27-143208
- Scope: historical+current
- Acceptance: critical 1/1, promotion 1/2, placement 1/3, maintenance 0/8
- Modify signals: user softened "NEVER" → "Avoid" in one style rule
- Detected pattern: maintenance-tier findings declined 0/8 → consider deprioritizing next run
```

If file exceeds 80 lines, summarize the oldest entries into `## Patterns` at the top, then delete the raw entries.

## Step 10 — Cleanup and ask

```bash
rm -rf "$SCRATCH"

# Prune old runs to the most recent 50
ls -1t "$RUNS_DIR"/*.json 2>/dev/null | tail -n +51 | xargs -r rm -f
```

Drifted/missing findings from pruned runs are lost — accepted tradeoff. If a finding still matters after 50 runs, it has either recurred (and will re-surface via current-conv signals) or genuinely become irrelevant.

Ask: `Commit the applied changes now? (y/n)` — if yes, hand off with the literal command:

```
/commit-only
```

Do not auto-commit. The user reviews the diff first.

</workflow>

<output_format>

Atomic. No narration. Print every shell command before running it (axiom 3). Findings are dense one-liners; full target paths inline. Tables for the audit summary and changes-applied table. No emojis except the audit-table status glyphs (✅ ⚠️) carried from the prior-improve scan.

</output_format>

<constraints>

## Rules

- Always full sweep. `$ARGUMENTS` weights priority, never narrows scope. **Why:** narrowing skips audits the user didn't know to ask for; the value is surfacing the unknown unknowns.
- Indexed selection (axiom 4) when finding count >4. `AskUserQuestion` only for per-item Accept/Modify/Skip (or Open-issue/Skip for atomic-meta tier) after the indexed pick. **Why:** paginating 12 findings through 4-option widgets is worse UX than one printed list.
- Atomic-meta findings never write files in this command. They route to `/report-issue-with-atomic` — the user runs it. **Why:** issue creation is a public action with persistent state on GitHub; per axiom 3 (destructive ops), the user invokes the publishing verb themselves so they can edit the body before submission.
- Per-item confirm before any file write or hook patch (axiom 3). No "apply all" shortcut. **Why:** advisory rules getting promoted to enforcement hooks change tool behavior globally; batch-accepting hides the blast radius.
- Memory consolidation: never delete a memory file before grep-verifying that the content lives in the target. The consolidate-then-clean order is mandatory. **Why:** unverified redundancy claims silently destroy rules.
- Skill budget warnings are **always Maintenance tier**, never Critical, unless the user has reported actual invisible skills. The 16K char ceiling is community-discovered, not documented by Anthropic. **Why:** false-positive Critical alarms erode trust in the audit.
- Prior-improve drifted/missing findings re-enter the current run at tier 1. Previously skipped OR suppressed findings re-enter at tier 2 only when their `signal_keywords` match a current-run signal. **Why:** the audit-trail is itself a deliverable; users want confidence that past accepts stuck, and skipped/suppressed items shouldn't nag unless the underlying friction is still present. Suppressed re-surfaces are weighted equally with skipped re-surfaces — the user never had the chance to actively decline a suppressed item.
- Never edit `~/.claude/settings.json` or `.claude/settings.json` without printing the JSON patch first and confirming. Delegate to `/update-config` skill when present. **Why:** settings.json changes affect tool permissions and hook execution — they need a visible diff.
- No commits. End by suggesting `/commit-only`; let the user inspect first. **Why:** mixed audit + ship is opaque; separating them keeps the diff reviewable.
- Read-only agents only — discovery, history scan, prior-improve audit all use read-only agents (`atomic-investigator`, `atomic-haiku`). Only the orchestrator writes. **Why:** parallel agents writing the same files is a race condition without coordination overhead.
- Atomic-tier carve-out for state: `improve-runs/` and `improve-learnings.md` live in `~/.claude/.atomic/`, not in memory (axiom 2 carve-out for shell-readable durable state). **Why:** the next run needs to grep past run logs deterministically; memory is conversational and not addressable from a shell.

## Open behaviors

- Skip Phase 5b (pattern promotion) entirely in current-only scope — requires cross-session history.
- Skip prior-improve audit on first run (when `$RUNS_DIR` is empty).
- Run-log JSON schema is informal — only `decisions[].target_file` and `decisions[].verify_phrase` are load-bearing for prior-improve audits. The rest is informational.
- `atomic-strategist` dispatch (opus, read-only) is *not* part of the default pipeline. Only invoke when the cross-conversation pattern is genuinely ambiguous — multiple plausible root causes for the same recurring friction. Don't dispatch on clear-cut signals; opus is expensive.

</constraints>
