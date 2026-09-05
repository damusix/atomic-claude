{{define "review-gate"}}<review-gate>
Give code the main agent wrote itself an independent review before it lands.

1. **Already-reviewed guard.** Skip when this change has already met a reviewer:
   - The staged work came from `/implement`, `/subagent-implementation`, `/quick-fix`, `/autopilot`, or `/subagent-diagnose` in this session. Each dispatches `atomic-reviewer` per iteration or per checkpoint, so the diff has been read by a fresh context already.
   - This gate already ran on the same change earlier in the flow — a squash of commits the commit-time gate reviewed, or a ship verb escalating after its own commit step. Review once per change, not once per step.
2. **Docs-only guard.** Run `git diff --cached --name-only` and apply the documentation-path test from the signals refresh step of this same flow — same paths, same treatment of bundled artifact `.md` files as source rather than docs. If every staged path is documentation, skip: prose belongs to the doc-impact step, not to a code reviewer.
3. **Dispatch** `atomic-reviewer` (`subagent_type: "atomic-reviewer"`) in code mode on the staged diff. Brief it with the diff range (`git diff --cached`), what the change was meant to do, and the spec path if one exists. There is no spec for ad-hoc work — say so rather than inventing one; the reviewer falls back to intent-versus-diff review.
4. **Act on the verdict before committing.**
   - `VERDICT: PASS` → continue.
   - Any 🔴 bug → fix it, then re-stage. Do not commit around a red finding.
   - 🟡 risk → fix it, or state in one line why it stands. Never carry one silently.
   - 🔵 nit → the user's call; mention them, do not block.
5. Report the totals line to the user before writing the commit message.

The main agent editing code directly is the third implementation path, alongside the subagent loop and `/quick-fix`. `/implement` is that path run deliberately, and it carries its own per-checkpoint gate; this step covers the same editing done ad-hoc, outside any command. **Why:** the agent that wrote the code is the worst judge of it — it reviews its own reasoning rather than the diff, and every rationalization that produced the bug is still in its context.
</review-gate>{{- end}}
