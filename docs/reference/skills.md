# Skills

Skills fire automatically when Claude encounters matching phrases in conversation. You do not need to invoke them — they activate on their own. You can also trigger them explicitly if you want.


## Discipline

These enforce good engineering habits without you having to ask.

| Skill | Fires when you say... | What it does |
|-------|----------------------|-------------|
| `atomic-tdd` | "let's implement X", "add feature Y", "fix bug Z" | Writes a failing test before touching production code. |
| `atomic-verify` | "done", "fixed", "passing", "ready to merge" | Runs verification before letting Claude claim completion. No evidence, no claim. |
| `atomic-debug` | pastes an error, "broken", "doesn't work", "failing" | Drives hypothesis-driven debugging instead of symptom-patching. |


## Workflow

These handle the craft of committing, reviewing, and documenting.

| Skill | Fires when you say... | What it does |
|-------|----------------------|-------------|
| `atomic-commit` | "write a commit", "commit message", or automatically from ship commands | Generates a Conventional Commits message. Subject under 50 chars, body only when the "why" is not obvious. |
| `atomic-review` | "review this PR", "code review", "review the diff" | Produces compressed review comments. One line per finding: location, problem, fix. |


## Awareness

These keep Claude and your docs in sync with the project.

| Skill | Fires when you say... | What it does |
|-------|----------------------|-------------|
| `atomic-prose` | "draft the README", "write the docs", "edit the guide" | Applies a clear, direct voice to narrative documentation. No marketing language, no AI-tell phrases. |
| `atomic-documentation` | "doc this change", "what surfaces does this touch" | Figures out which docs need updating based on a diff and routes each to the right voice. |
