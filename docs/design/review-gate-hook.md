# Review gate for main-agent code: a Stop hook that fires


## Problem


When the main agent writes code itself, outside `/implement`, `/subagent-implementation`, `/quick-fix`, `/autopilot`, and `/subagent-diagnose`, it says "done" and no review agent runs. `context/CLAUDE.md` says ad-hoc edits get their review gate at the exits (`atomic-verify` before "ready", the ship verbs before the commit), and `context/skills/atomic-verify/SKILL.md` already carries the rule verbatim: "dispatch `atomic-reviewer` on the diff before claiming the work is ready". Seven user complaints between 2026-09-02 and 2026-09-08 (issue #271) say the gate does not fire.

The gate is prose, and prose that asks the model to notice its own transition from "doing" to "asserting done" is the weakest trigger the system has. Issue #269 records three rewrites of the sibling comment-discipline rule with no change in outcome. A fourth rewrite of this one is not a fix.

Two other things are wrong with the gate as written:

- It dispatches `atomic-reviewer`, which is pinned to `claude-sonnet-5`. The user wants the ad-hoc gate on the session's model (Opus or Fable). `atomic-auditor` has no `model:` and inherits the caller's, but its contract says "dispatched exactly once after the implement-review loop" and "not a diff reviewer".
- Nothing outside the model can tell whether the gate ran. `/subagent-implementation` and the other loops have an orchestrator step that dispatches the reviewer; ad-hoc work has only the skill's auto-trigger.


## Goals / Non-goals


- Goals:
    - A deterministic trigger: when the main agent edited code this session and no review agent has read the result, the turn does not end silently.
    - The gate runs on the session's model: `atomic-auditor`, widened to admit a working-diff audit without loosening its once-per-loop contract.
    - The completion message names the pass and the model that ran.
    - The trigger never blocks on its own failure: no git, no state, malformed input, or any error is a silent exit 0.
    - Subagent edits do not count as main-agent edits; a loop that already reviewed per checkpoint is not re-gated.
- Non-goals:
    - Verdict enforcement in code. The hook knows a review happened, not what it said; acting on `CHANGES_REQUESTED` stays with the skill prose, as it does in every loop.
    - Gating user edits. Files the user changed by hand are not Claude's to review.
    - A second review of a change the ship verbs' gate already read in the same flow.
    - Transcript parsing. The hook keeps its own state; it never reads `transcript_path`.


## Approaches


| # | Approach | Pros | Cons |
|---|----------|------|------|
| A | Prose only: `atomic-verify` dispatches `atomic-auditor` instead of `atomic-reviewer` | One skill edit, no Go | Same trigger that failed seven times; #269 shows three rewrites of a sibling rule changed nothing |
| B | Stop hook parsing `transcript_path` for Edit/Write and Agent tool uses | One hook registration, no state file | The transcript is written asynchronously and may lag the turn (hooks reference), so the review that just ran can be missing and the hook blocks falsely; the JSONL shape is undocumented and can drift |
| C | Stop hook over state a PostToolUse hook keeps: paths the main agent edited, and a fingerprint of those paths taken when a review agent was dispatched | Deterministic and synchronous: PostToolUse fires when the tool returns; `agent_type` in the input separates subagent edits from the main agent's; one small state file per session in the OS temp dir | Two hook registrations and two new `atomic hooks` verbs; a `git status` and a `git diff` per stop |


## Recommendation


C, with A's prose change folded in, because the prose still has to say what the gate does once it fires.

The harness runs hooks; the model does not get to forget them. Claude Code's hooks reference (verified 2026-09-20) gives every field the mechanism needs:

- `PostToolUse` input carries `tool_name`, `tool_input`, `session_id`, `cwd`, and, for a tool call made inside a subagent, `agent_id` and `agent_type`. Hooks fire inside subagents, so the `agent_type` check is what keeps `atomic-implementer`'s edits out of the main-agent set.
- The subagent tool is `Agent`; its `tool_input.subagent_type` names the agent. `Edit`, `Write`, `MultiEdit`, and `NotebookEdit` carry `tool_input.file_path`.
- `Stop` input carries `session_id`, `cwd`, and `stop_hook_active`. A hook that exits 2 blocks the turn from ending and Claude sees stderr. When `stop_hook_active` is true the hook must exit 0, so a block costs at most one forced continuation per turn.

Two verbs, one state file:

```
PostToolUse (Edit|Write|MultiEdit|NotebookEdit|Agent) -> atomic hooks post-tool-use
    agent_type present            -> exit 0 (subagent tool call)
    Edit/Write/...                -> touched += file_path
    Agent, subagent_type in
      {atomic-reviewer, atomic-auditor} -> reviewed = fingerprint(touched)

Stop -> atomic hooks stop
    stop_hook_active              -> exit 0
    no state for session_id       -> exit 0
    dirty = touched ∩ git-dirty ∩ not-docs
    dirty empty                   -> exit 0
    fingerprint(dirty) == reviewed -> exit 0
    else                          -> exit 2, stderr names the paths and the gate
```

`fingerprint` is a hash over `git diff HEAD -- <paths>` plus the bytes of any untracked path in the set, so a fix after a review re-opens the gate and a commit closes it (nothing dirty). The docs test is the one the ship verbs' signals gate uses: under a `docs/` directory at any depth, or a top-level `README*`, `CHANGELOG*`, `CONTRIBUTING*`, `CODE_OF_CONDUCT*`, `SECURITY*`, `LICENSE*`. Bundled-artifact `.md` files count as source, as they do there.

State lives at `<os temp dir>/atomic-hooks/<session_id>.json`. It is per session and worthless after it, so the OS temp dir is the right home: nothing under `~/.atomic` to prune, and macOS and Linux both reap it.

Why the auditor and not the reviewer: `context/agents/atomic-reviewer.md:15` pins `model: claude-sonnet-5`; `context/agents/atomic-auditor.md` leaves `model:` unset (`docs/spec/atomic-auditor.md`, 2026-08-21 entry), so it runs on whatever the session runs on. The auditor's contract widens by one caller-provided mode, `diff: working`, in which it audits the uncommitted tree against the caller's stated intent instead of a range against a spec. Its loop contract (once per loop, after the reviewer went green, never a checkpoint diff) is untouched: a working-diff audit is not a checkpoint of any loop. The one-dispatch rule is scoped to the loop; in working-diff mode every gate event is its own dispatch, because a fix after `CHANGES_REQUESTED` re-opens the hook and the only way to close it is another read. Both gates read the working tree (`git diff HEAD` plus untracked files), the same scope the hook fingerprints, so `atomic-verify`'s gate, the ship verbs' gate, and the hook agree on what "reviewed" covers.

The ship verbs' review gate (`context/_partials/review-gate.md`) moves to the same agent for the same reason, and its already-reviewed guard learns one more case: `atomic-verify`'s gate already ran the auditor on this change this session. One agent gates ad-hoc main-agent code at both exits; the hook accepts either agent as a review, because the loops still dispatch the reviewer per checkpoint and those must not be re-gated.

Why not gate on the verdict: the hook records that a review agent was dispatched, not what it returned. A `CHANGES_REQUESTED` that Claude fixes re-dirties the paths, the fingerprint moves, and the next stop blocks once more. That is the right pressure without a second parser for the reviewer's output.


## Open questions


- Whether `AskUserQuestion` mid-turn fires `Stop`. If it does, a clarifying question with unreviewed edits costs one nudge; the skill's boundary ("intent statements are future, no gate yet") tells Claude to say what is in progress and continue.
