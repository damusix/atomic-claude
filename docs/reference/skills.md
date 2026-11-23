# Skills

Skills fire automatically when the assistant encounters matching phrases in conversation. You do not need to invoke them — they activate on their own. You can also trigger them explicitly if you want.


## Discipline

These enforce good engineering habits without you having to ask. Together, `atomic-tdd` and `atomic-verify` are the loop's objective gate: a test that passes or fails, not a second opinion. That is the difference between a loop that catches its own mistakes and one that talks itself into "done". Outside the loop, where the assistant edits code directly and no reviewer is watching, `atomic-verify` restores the second opinion the loop has by default: it dispatches `atomic-reviewer` on the diff before the work can be called ready.

| Skill | Fires when you say... | What it does |
|-------|----------------------|-------------|
| `atomic-tdd` | "let's implement X", "add feature Y", "fix bug Z" | Writes a failing test before touching production code. |
| `atomic-verify` | "done", "fixed", "passing", "ready to merge" | Matches fresh evidence to the claim. A targeted check proves the changed behavior; a full-suite claim needs the full suite. Code written directly outside the implementation loop also needs independent review, scoped to the task. |
| `atomic-debug` | pastes an error, "broken", "doesn't work", "failing" | Drives hypothesis-driven debugging instead of symptom-patching. |


## Workflow

These handle the craft of committing, reviewing, and documenting.

| Skill | Fires when you say... | What it does |
|-------|----------------------|-------------|
| `atomic-git-discipline` | Commit messages, PR descriptions, or requests to ship | Uses conventional messages and provides an on-demand reference for commit, push, PR, merge, and squash. |
| `atomic-review` | "review this PR", "code review", "review the diff" | Shared rules for correctness, reuse, readable code and comments, and actionable findings. The reviewer and auditor apply the same severity rules at different scopes. |
| `atomic-planning` | Planning or testing a proposal | Selects the method that resolves the main uncertainty. Loads evidence, pressure-test, or challenge-swarm guidance only when needed. |
| `atomic-retro` | `/retro`, "review this session", "check whether our earlier retro changes stuck" | Proposes evidence-backed improvements for approval. Reads past sessions or checks prior decisions only when requested. |

For visual decisions, `/atomic-plan` presents labeled HTML variants and records the user's choices in the design.


## Awareness

These keep the assistant and your docs in sync with the project.

| Skill | Fires when you say... | What it does |
|-------|----------------------|-------------|
| `atomic-writing` | "draft the README", "write the docs", "edit the guide", "write the spec" | The one voice for every file the repo ships, from READMEs to specs to agent prompts. Length follows the surface; a diagram beats a paragraph wherever the content has a shape. `references/mermaid.md` sets layout limits (direction, width, density, color) so a diagram stays readable in light and dark themes. No marketing language, no AI-tell phrases. |
| `atomic-documentation` | "doc this change", "what surfaces does this touch", or implementation maintenance | Finds affected docs. Standalone requests keep per-page choices; implementation uses the scratch scan and task diff to make required updates before audit. |
| `atomic-wiki` | Bucket setup, capture, indexing, or queries | Routes to usable capture buckets, protects private registrations, and answers explicit wiki queries. Repo wiki refresh is manual. |
| `atomic-bus` | "join the bus", "connect to the bus", "message the backend session", "delegate this to the frontend session", "coordinate with the other agent", "watch the room", "halt the room" | Joins a named room (`atomic bus join`) and starts a `recv` Monitor so peer messages arrive as prompts. Carries the reaction policy: act on messages addressed to you, note FYI traffic, never act unaddressed. |
