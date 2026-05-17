# Skills


Skills auto-fire when Claude encounters matching phrases. They can also be invoked explicitly.

| Skill | When it fires |
|-------|--------------|
| `atomic-commit` | "write a commit", "commit message", commit-time invocation from ship commands. |
| `atomic-review` | "review this PR", "code review", "review the diff". |
| `atomic-debug` | Error pastes, "broken", "doesn't work", "failing". |
| `atomic-tdd` | "let's implement X", "add feature Y", "fix bug Z", pre-code-change phrases. |
| `atomic-verify` | "done", "fixed", "passing", "ready to merge", "looks good" — any completion claim. |
| `atomic-signals` | "regenerate signals", "scan the project", "refresh project context", "what's in this repo", "rescan". Also fires from `/commit-only` when staged diff touches source files. |
