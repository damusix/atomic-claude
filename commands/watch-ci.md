---
description: Spawn a Haiku subagent in the background to watch CI for the current branch (or specified target). Provider-agnostic — the subagent inspects project signals to identify the CI system (GitHub Actions, GitLab CI, CircleCI, etc.) and picks the right CLI. Returns immediately; reports back when CI reaches a terminal state.
---

## Step 1 — Pre-flight

```bash
git rev-parse --is-inside-work-tree
```

If not inside a git repo, stop:

```
not a git repo. /watch-ci needs a versioned project with a CI provider.
```

Check signals are available — the subagent uses them to identify the CI provider:

```bash
test -f .claude/project/inferred-signals.md
```

If missing, warn (don't stop):

```
note: no inferred-signals.md found. subagent will fall back to file-tree heuristics
(.github/, .gitlab-ci.yml, .circleci/, Jenkinsfile, etc.).
suggest running /initialize-signals or /refresh-signals after to improve future runs.
```

## Step 2 — Capture context for the brief

```bash
git rev-parse --abbrev-ref HEAD     # current branch
git rev-parse HEAD                  # current SHA
git remote -v | head -1             # remote URL — provider hint
pwd                                  # absolute repo path
```

`$ARGUMENTS` may narrow the target. Pass it through verbatim — the subagent classifies it using provider-appropriate semantics:

- empty → current branch's latest run(s)
- integer → run / pipeline ID
- `#N` or `N` → PR / MR number
- branch name → that branch's runs
- workflow / pipeline filename (`.yml`, `.yaml`) → that specific workflow

Don't classify in the foreground. Different providers use different shapes (GitHub run IDs, GitLab pipeline IDs, CircleCI workflow UUIDs), so the subagent decides.

## Step 3 — Dispatch background subagent

Invoke the `Agent` tool with:

- `subagent_type: "atomic-haiku"` — generic Haiku-backed runner; model is set in the agent's frontmatter, not as a per-call parameter.
- `run_in_background: true`
- `description: "Watch CI for <branch-or-target>"`
- `prompt`: a self-contained brief containing:

    ```
    Watch CI for this repo and report when it reaches a terminal state.

    Repo: <absolute pwd>
    Branch: <current branch>
    HEAD SHA: <current SHA>
    Remote: <remote URL>
    Target hint ($ARGUMENTS): "<verbatim args, or 'current branch' if empty>"

    Step 1 — identify the CI provider:
      a. Read .claude/project/inferred-signals.md if it exists. The inferrer's schema
         includes a CI/CD section that names the provider and points at config files.
         Use it as the primary source of truth.
      b. If signals are absent or silent on CI, fall back to file-tree detection:
         .github/workflows/*.yml      → GitHub Actions    (use `gh`)
         .gitlab-ci.yml               → GitLab CI         (use `glab`)
         .circleci/config.yml         → CircleCI          (use `circleci`)
         Jenkinsfile                  → Jenkins           (curl against $JENKINS_URL)
         .buildkite/pipeline.yml      → Buildkite         (use `bk`)
         bitbucket-pipelines.yml      → Bitbucket Pipelines (curl REST API)
         azure-pipelines.yml          → Azure Pipelines   (use `az pipelines`)
      c. If no provider is identifiable, bail: print `no CI provider detected.` and stop.

    Step 2 — verify the CLI is available:
      `command -v <cli>` and (where applicable) auth check (e.g. `gh auth status`).
      If missing or unauthed: print `detected <provider>, but <cli> not installed/authed.
      install: <command>` and stop. Don't try to fall back to a different tool.

    Step 3 — resolve the target using $ARGUMENTS-equivalent semantics for the provider:
      - empty / "current branch" → runs/pipelines on <branch> @ <SHA>
      - integer → specific run/pipeline ID
      - `#N` / `N` → PR/MR number; resolve to head SHA, then watch runs on that SHA
      - branch name → runs on that branch
      - `.yml`/`.yaml` → specific workflow/pipeline file on <branch>
      Print one line: `watching <provider>: <resolved target>`.

    Step 4 — poll until terminal state:
      Cadence 30-60s. Use streaming watch where the provider supports it
      (`gh run watch <id> --exit-status`, `glab ci view --live <id>`, etc).
      Cap total wait at 10 minutes. If still running, report current state and stop.
      READ-ONLY. Do not rerun, cancel, or modify anything.

    Step 5 — report concisely:
      - Per workflow/pipeline: outcome (success/failure/cancelled/timed-out).
      - On failure: failing step/job name + 1-3 line error excerpt from logs.
      - Note any side effects (e.g. release-please opened a PR, deployment URL).
      - One paragraph total. The user is in another conversation; respect their context.
    ```

Do NOT block on the agent. Return control to the user immediately.

## Step 4 — Hand back

Print one line confirming the dispatch:

```
/watch-ci dispatched. continue working; you'll get a notification when CI settles.
```

When the agent completes, pass-through summarize its report in 1-3 lines.

---

## Rules

- Provider detection lives in the subagent, not the command. Signals + tree heuristics, in that order.
- Stop on pre-flight failure (not a git repo). Warn but continue on soft failures (signals missing).
- Default to read-only. The watcher never re-runs, cancels, or modifies workflows.
- One target per invocation. If `$ARGUMENTS` is ambiguous for the detected provider, the subagent asks via printed clarification (it can't `AskUserQuestion` from background, so it bails with a question instead).
- Always `run_in_background: true`. The point of this command is non-blocking observation.
- Dispatched agent is `atomic-haiku` — generic Haiku runner. Model is set in the agent's frontmatter; never pass `model:` as a per-call Agent parameter (it's silently ignored).
- Cap the wait at 10 minutes. CI that exceeds that needs human investigation, not infinite polling.
- Do not poll the background-agent's output file from the foreground. The harness notifies on completion.
