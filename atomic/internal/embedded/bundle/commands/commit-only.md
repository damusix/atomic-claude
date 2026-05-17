---
description: Stage and commit current changes. Delegates message format to the atomic-commit skill. Does not push.
---

1. Invoke the `atomic-commit` skill. Follow it for message format.
2. `git status`, `git diff`, `git log -n 10 --oneline` (parallel).
3. Stage relevant files explicitly by path. No `git add -A` / `.`. Skip secrets, build artifacts, large binaries. If staged/unstaged intent is ambiguous, ask.
4. **Signals pre-commit** — evaluate these gates in order; stop at the first that fails:
    1. `git diff --cached --name-only` matches either:
        - a source extension: `.ts .tsx .js .jsx .py .go .rs .rb .java .c .cc .cpp .h .hpp .swift .kt .php`, **or**
        - a known manifest filename: `package.json tsconfig.json Cargo.toml pyproject.toml requirements.txt Gemfile composer.json pom.xml build.gradle build.gradle.kts go.mod go.sum`.

        If neither matches (prose-only: `.md`, generic `.yml .yaml .json .toml` that isn't a manifest), skip the step entirely.
    2. `command -v atomic` succeeds? If not, skip.
    3. `atomic signals stale` exits 1 (stale)? If it exits 0 (fresh), skip.

    All three pass → invoke the `atomic-signals` skill in silent mode (no report line). If signals regenerate, stage `.claude/project/deterministic-signals.md` and `.claude/project/inferred-signals.md`.
5. Commit using a HEREDOC message.
6. `git status` to confirm.

On pre-commit hook failure: fix root cause, re-stage, create a NEW commit. No `--no-verify`. No `--amend`.

No push. No PR. One commit per invocation — if diff spans unrelated concerns, ask how to split.
