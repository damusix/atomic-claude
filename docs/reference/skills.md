# Skills

Skills fire automatically when Claude encounters matching phrases in conversation. You do not need to invoke them — they activate on their own. You can also trigger them explicitly if you want.


## Discipline

These enforce good engineering habits without you having to ask. Together, `atomic-tdd` and `atomic-verify` are the loop's objective gate: a test that passes or fails, not a second opinion. That is the difference between a loop that catches its own mistakes and one that talks itself into "done". Outside the loop, where Claude edits code directly and no reviewer is watching, `atomic-verify` restores the second opinion the loop has by default: it dispatches `atomic-reviewer` on the diff before the work can be called ready.

| Skill | Fires when you say... | What it does |
|-------|----------------------|-------------|
| `atomic-tdd` | "let's implement X", "add feature Y", "fix bug Z" | Writes a failing test before touching production code. |
| `atomic-verify` | "done", "fixed", "passing", "ready to merge" | Runs verification before letting Claude claim completion. No evidence, no claim. On code Claude wrote directly, outside the implement loop, verification includes an `atomic-reviewer` pass. |
| `atomic-debug` | pastes an error, "broken", "doesn't work", "failing" | Drives hypothesis-driven debugging instead of symptom-patching. |


## Workflow

These handle the craft of committing, reviewing, and documenting.

| Skill | Fires when you say... | What it does |
|-------|----------------------|-------------|
| `atomic-git-discipline` | "write a commit", "commit message", "open a PR", "PR description", or automatically from ship commands | Generates a Conventional Commits message. Subject under 50 chars, body only when the "why" is not obvious. Also owns PR titles and bodies: only what the diff can't show, about 120 words max. |
| `atomic-review` | "review this PR", "code review", "review the diff" | Produces compressed review comments. One line per finding: location, problem, fix. |
| `atomic-visual-options` | "show me options", "mock up variants", "let me see this side by side", "compare these layouts" | Renders 2–4 visual variants per decision dimension as a throwaway self-contained HTML file; user picks by typing codes (e.g. `A2 B3`); chosen option recorded in the design doc. Invoked by `/atomic-plan` when a design question is visual. |


## Awareness

These keep Claude and your docs in sync with the project.

| Skill | Fires when you say... | What it does |
|-------|----------------------|-------------|
| `atomic-writing` | "draft the README", "write the docs", "edit the guide", "write the spec" | The one voice for every file the repo ships, from READMEs to specs to agent prompts. Length follows the surface; a diagram beats a paragraph wherever the content has a shape. No marketing language, no AI-tell phrases. |
| `atomic-documentation` | "doc this change", "what surfaces does this touch" | Figures out which docs need updating based on a diff and routes each to the right voice. |
| `atomic-wiki` | "I want a place for notes/tickets/research", "add a bucket", "add this to my wiki", "wiki this", "what does my wiki know about X", "is my wiki stale", "set up a karpathy wiki" | Routes capture-folder intent to `atomic wiki bucket add` (never a bare mkdir), handles karpathy-realm setup, and answers staleness queries for registered wikis. |
| `atomic-bus` | "join the bus", "connect to the bus", "message the backend session", "delegate this to the frontend session", "coordinate with the other agent", "watch the room", "halt the room" | Joins a named room (`atomic bus join`) and starts a `recv` Monitor so peer messages arrive as prompts. Carries the reaction policy: act on messages addressed to you, note FYI traffic, never act unaddressed. |
