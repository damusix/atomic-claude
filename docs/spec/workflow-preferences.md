# Workflow preferences


## Goal


Every session knows which source-control provider, issue tracker, and worktree location to use, resolved from the user profile, the realm, and the repository, and reads the matching `atomic-git-discipline` reference before acting. Follow-ups land in the chosen tracker, with the local ledger as the fallback. Atomic has no reminder feature.

A provider is a host family throughout this spec: GitHub Enterprise is `github`, and `source-control-host` names the instance.


## Non-goals


- Atomic code that calls a provider API. Reference files tell the model what to run.
- Authenticating to a provider or storing a token.
- Shipping a provider reference that has not been checked against vendor documentation.
- Moving existing follow-up entries into a tracker automatically. `/follow-up review` promotes one entry on request.
- Reminders in any form, including a replacement scheduler.
- Choosing a CI provider. `/watch-ci` keeps file-tree CI detection, and `/subagent-diagnose` uses the same detection.
- A `bitbucket` issue tracker. Bitbucket Cloud is source control only.
- Deleting existing reminder files. They stay on disk, unread.
- Changing the local follow-up ledger: the `atomic/internal/followups` package, its five verbs, its folder (`<state-root>/project/followups`, printed by `atomic followups path`), and doctor check 6 stay as they are.
- The OMP extension handler, the Codex `hooks.json` entry, the `--harness` and `--cwd` flags on `atomic hooks session-start`, and the `worktrees` field of `atomic where --json`. `docs/spec/omp-plugin-compatibility.md` owns them, along with the state root, harness detection, and the worktree directory helper.
- Changing `/report-issue-with-atomic`, which targets this project's own GitHub repository regardless of preference.


## Success criteria


**Block grammar and refusal rules**

- [ ] A preference block is a line-anchored `<workflow-preferences level='<level>'>` open tag (single, double, or no quotes around the value) and a `</workflow-preferences>` close tag, outside fenced code. Lines inside are `- <key>: <value>` with an optional `# <note>` suffix.
- [ ] Keys and values are exactly: `source-control` (`github`, `gitlab`, `bitbucket`, `codeberg`, `none`); `source-control-host` (a hostname); `issue-tracker` (`github`, `gitlab`, `codeberg`, `linear`, `jira`, `trello`, `filesystem`); `issue-tracker-project` (a non-empty token); `worktrees` (`auto`, `off`, or a repository-relative path with no `..` segment).
- [ ] `- key:` with an empty value is unset. `none` and `filesystem` are values and stop the fall-through.
- [ ] A line carrying `# detected` is a soft value. Any other set line is explicit.
- [ ] Two blocks in one file, or an unpaired tag, is a refusal: the resolver skips that level with a warning, and no writer touches the file.
- [ ] A `level` attribute that disagrees with the file's location, or a missing `level` attribute, produces a warning; the location decides the level.
- [ ] An unknown key, an invalid value, or a key repeated within one block produces a warning. The unknown key is ignored, the invalid value is unset at that level, and the first of the repeated lines wins.
- [ ] Locations: profile is the block inside `## Workflow preferences` in `~/.atomic/profile.md`; realm is `<realm-root>/<state-root>/workflow-preferences.md`; repo is `<repo-root>/<state-root>/workflow-preferences.md`, with `<state-root>` resolved as `docs/spec/omp-plugin-compatibility.md` defines.

**Resolution and warnings**

- [ ] Levels are consulted in the order repo, realm, profile. The realm level is skipped outside a registered realm.
- [ ] Per key, precedence is:
    1. The first explicit value.
    2. For `source-control`: the provider the `origin` host maps to, when it maps to one. For `source-control-host`: the `origin` host, only when its mapped provider equals the resolved `source-control`.
    3. For `issue-tracker`: the first soft value, which must be a non-host tracker (`linear`, `jira`, `trello`); else the tracker of the resolved `source-control` (`github`, `gitlab`, `codeberg`), or `filesystem` when it has none (`bitbucket`, `none`), at level `derived`.
    4. For every key except `issue-tracker`: the first soft value.
    5. The default.
- [ ] Detection only ever writes a non-host tracker as a soft `issue-tracker`. A hand-written soft host tracker (`- issue-tracker: github  # detected`) is invalid at that level: it is ignored with a warning.
- [ ] Defaults: `source-control` is `github`; `source-control-host` is the provider's public host; `issue-tracker-project` is unset; `worktrees` is `auto`.
- [ ] Outside a repository, or when the state root is ambiguous, the resolver skips the repo level and adds a warning; `resolve` still exits 0.
- [ ] Resolver test row: `source-control: gitlab (repo)` with `origin` at `github.com` resolves `source-control-host` to `gitlab.com (default)`, not `github.com`.
- [ ] Resolver test rows: no `origin` and a profile soft `source-control: gitlab` resolve to `source-control: gitlab (profile)`; a directory outside any repository resolves from realm and profile with one warning and exit 0.
- [ ] Host mapping: `github.com` and `*.ghe.com` map to `github`, `gitlab.com` to `gitlab`, `bitbucket.org` to `bitbucket`, `codeberg.org` to `codeberg`. Any other host maps to nothing. The `origin` URL is parsed in its `git@HOST:PATH`, `https://[user@]HOST/PATH`, and `ssh://git@HOST[:PORT]/PATH` forms, case-insensitively.
- [ ] Resolver test row: `origin` at `bitbucket.org` and a profile soft `source-control: github` resolve to `source-control: bitbucket (origin)` and `issue-tracker: filesystem (derived)`.
- [ ] An explicit `source-control` whose provider differs from the mapped `origin` provider produces a warning naming both.
- [ ] An `origin` host that maps to nothing produces a warning asking for an explicit `source-control` and `source-control-host`.
- [ ] The `worktrees` line comes from the one worktree directory helper CP10B of `docs/spec/omp-plugin-compatibility.md` defines: Claude maps to `.claude/worktrees`; OMP, Codex, and an unknown harness map to `<state-root>/worktrees`; an explicit preference path wins; `off` stays `off`. `atomic where --json` calls the same helper, and the resolver carries no second implementation.
- [ ] The resolved block has this shape, one line per set key with its source level (`repo`, `realm`, `profile`, `origin`, `derived`, `default`), omitting unset keys, then one `warning:` line per warning or `warning: none`, then a `Read:` line listing `references/source-control/<value>.md`, `references/issue-tracking/<value>.md`, and `references/worktrees.md`:

    ```text
    <workflow-preferences level='resolved'>
    - source-control: gitlab (realm)
    - source-control-host: git.acme.example (realm)
    - issue-tracker: jira (realm)
    - issue-tracker-project: ACME-BILLING (repo)
    - worktrees: .claude/worktrees (default)
    warning: none
    Read: references/source-control/gitlab.md, references/issue-tracking/jira.md, references/worktrees.md
    </workflow-preferences>
    ```

- [ ] The resolver reads all three levels on every call and caches nothing between calls.

**Autodetect**

- [ ] Detection writes only the `level='profile'` block, and only lines that are absent or carry `# detected`. It never writes a realm or repo file.
- [ ] A line without `# detected` is preserved byte for byte. So are unknown keys, prose, blank lines, and every byte outside the block.
- [ ] `source-control` is written when exactly one source-control provider is authenticated. Two or more leave the key unset and remove an earlier `# detected` line for it. None leaves the line as it is.
- [ ] `issue-tracker` is written only when exactly one non-host tracker credential (Linear, Jira, Trello) is present, with the same two-or-more removal rule. Host trackers are never written; they are derived from `source-control`.
- [ ] Test rows cover: one provider writes the line; two providers remove an earlier detected line; one non-host tracker writes `issue-tracker`; an explicit line survives every case unchanged.
- [ ] Credentials are read from each provider CLI's host configuration file at the location its vendor documentation names. Environment tokens are checked by variable name only; a value is never read, logged, or written.
- [ ] `gh`, `glab`, `tea`, `fj`, and `acli` are entries in the profile tool registry, so their presence also appears in `## Environment`.
- [ ] Every written line ends `# detected: <reason>`, for example `# detected: gh authed for github.com`.
- [ ] A missing `## Workflow preferences` section is inserted immediately before `## Environment`, or at the end of the file when there is no Environment heading. The stub profile ships the section with an empty block.
- [ ] A duplicate or unpaired profile block, or a block inside the `## Environment` span, is a refusal: refresh writes nothing to the file and prints the reason to stderr.
- [ ] Running detection twice with the same signals produces identical bytes, and an unchanged result writes nothing.
- [ ] Detection runs on every path that runs `profile.Refresh` and on the same daily cadence. The `origin` host is never written to the profile.

**CLI**

- [ ] `atomic workflow resolve [--cwd DIR] [--harness KIND] [--json]` prints the resolved block for `DIR` (default: the process directory). `--harness` takes the kinds defined in `docs/spec/omp-plugin-compatibility.md`; without it, harness detection applies.
- [ ] `resolve` exits 0 whenever it can render a block; warnings live inside the block. It exits 1 only when it cannot render one (for example, `DIR` does not exist) and 2 on a usage error, including an unknown `--harness` value.
- [ ] `resolve --json` prints `{"values":[{"key":"…","value":"…","level":"…"}],"warnings":["…"],"read":["…"]}` carrying the same facts as the text block; unset keys are absent from `values`.
- [ ] `atomic workflow set <key> <value> [--level profile|realm|repo]` writes one explicit line (no `# detected`) at the level, `profile` by default, replacing any existing line for that key and creating the file, section, or block when absent. It prints `wrote <path>`.
- [ ] `set` exits 2 on an unknown key or invalid value, and 1 when the level has no location (`--level realm` outside a realm, `--level repo` outside a repository), when the target file is a refusal case, or when the state root is ambiguous under `docs/spec/omp-plugin-compatibility.md`.
- [ ] When `workflow set` triggers a state migration (the trigger is wired by CP11 of `docs/spec/omp-plugin-compatibility.md`), the migration notice goes to stderr and stdout carries only `wrote <path>`.
- [ ] `cliusage` carries rows for both verbs and their flags, and the root verb table test lists `workflow`.

**Carriers and fallback**

- [ ] The session-start builder emits a `## Workflow preferences` section holding the resolved block after `## Orientation`, resolved for the `--harness` flag and the directory resolved from `--cwd`, stdin `cwd`, or the process directory, as CP9A of `docs/spec/omp-plugin-compatibility.md` defines. Claude registration passes `--harness claude` only.
- [ ] On Claude, the section reaches the model in the `SessionStart` hook's `additionalContext`.
- [ ] A resolver failure omits the section and the hook still exits 0. The hook never blocks the session.
- [ ] `context/AGENTS.md` carries this paragraph verbatim, immediately after the `## User profile` section. It is an LLM safeguard layer over a deterministic path that can be absent (hook not installed, Codex hook not trusted, `atomic` not on the harness PATH), and it is the only Codex carrier until the Codex hook is trusted:

    ```text
    ## Workflow preferences

    Source control, issue tracking, and worktree placement follow the `<workflow-preferences level='resolved'>` block the session-start hook adds to context; it outranks any other `<workflow-preferences>` block you read. If no resolved block is in context, run `atomic workflow resolve` once, silently, and use its output. Before a change-request, issue, follow-up, or worktree operation, read the `atomic-git-discipline` references the block lists under `Read:`.
    ```

**References**

- [ ] `context/skills/atomic-git-discipline/references/` holds `resolution.md`, `worktrees.md`, `source-control/{github,gitlab,bitbucket,codeberg,none}.md`, and `issue-tracking/{github,gitlab,codeberg,linear,jira,trello,filesystem}.md`.
- [ ] Every source-control file answers every row: `default-branch`, `change-request-view`, `change-request-create`, `change-request-edit-title`, `change-request-merge` (merge, squash, delete branch), `change-request-attach-images`, `push-permission-check`, `ci-target-resolve`, `auth-check`, `install-hint`, `closing-keywords`.
- [ ] Every issue-tracking file answers every row: `issue-create`, `issue-view`, `issue-search`, `issue-close`, `label-list`, `label-apply`, `template-location`, `commit-reference-syntax`, `project-identifier`.
- [ ] A row with no CLI says `unsupported` and gives the REST call. A row with no REST call either (`none`, `filesystem`) says `unsupported` and why.
- [ ] Every operation row carries the vendor documentation URL it was checked against, or the word `unverified`.
- [ ] The GitHub, GitLab, and Codeberg issue-tracking files point to the matching source-control file for install and authentication. `filesystem.md` resolves the ledger folder with `atomic followups path` and never names it, and it is the one reference that carries the `atomic followups add` command.
- [ ] Bypass flags (`--admin`, `force_merge`, an auto-merge override) are named only as never run unattended.
- [ ] `SKILL.md` carries a short index naming every reference as a backticked `references/...` path. Every file under the skill folder ships, and each `references/...` or `./...` path a shipped file declares resolves against that file's own directory; an unresolved path fails the OMP package and Codex plugin builds.
- [ ] Pointers between files inside `references/`, including those in `resolution.md`, use a path relative to the declaring file (`../source-control/github.md` from `issue-tracking/`), never `references/source-control/...`. A test asserts that every `../` target named in `references/**` exists in the shipped set.
- [ ] `SKILL.md` triggers cover opening a change request, filing an issue, recording a follow-up, choosing a tracker, and creating a worktree, and name no provider CLI.
- [ ] `SKILL.md`'s screenshots section points to the `change-request-attach-images` operation, and its boundary line names no provider CLI.
- [ ] The skill description names the commands and partials that invoke it.
- [ ] The Claude and OMP skill projections carry every reference at its nested path.

**Wired commands**

- [ ] `base-resolution` resolves the default branch with `git symbolic-ref refs/remotes/origin/HEAD`, then git configuration, and reads the source-control reference's `default-branch` row only when both fail. `review-branch`, `git-cleanup`, `atomic-help`, and `pr-flow` compose it instead of inlining a copy.
- [ ] `pr-flow`, `merge-flow`, `commit`, `watch-ci` (change request to head SHA), `report-issue`, and `autopilot` (issue input) name an operation and tell the model to read the reference the resolved block lists; none names a provider CLI command.
- [ ] `report-issue-privacy` carries no provider operation: it redacts, previews, and confirms only.
- [ ] `/report-issue-with-atomic` still targets `damusix/atomic-claude` on GitHub through `gh`, regardless of preferences.
- [ ] Ship verbs and `/report-issue` print `source-control: <value> (<level>)` or `issue-tracker: <value> (<level>)` before their first provider operation. Destructive confirmations are unchanged.
- [ ] With `source-control: none`, `/commit merge` and `/commit squash merge` take the local-merge path, and `/commit push` and `/commit pr` refuse and tell the user to add a remote or change the preference.
- [ ] `/subagent-diagnose`'s `ci` source uses the `/watch-ci` provider detection, not a hard-coded `gh run` command.
- [ ] `worktree-setup`, `worktree-cleanup-prompt`, and `setup-wiki`'s ignore-rule check use the `worktrees` field of `atomic where --json` and the worktrees reference. `setup-wiki` probes the ignore rule for the resolved `worktrees` location and remediates with `atomic repo init`, which ignores `<state-root>/worktrees/`, and `.claude/worktrees/` when `.claude/` exists or a Claude target is enrolled, per CP10B of `docs/spec/omp-plugin-compatibility.md`. For an explicit `worktrees` path, `setup-wiki` reports the exact `.gitignore` line to add instead. Cleanup and scans enumerate with `git worktree list`, never a path glob.
- [ ] `atomic-help` computes `in_worktree` from the git-dir and git-common-dir test, not from a path substring.

**Follow-up sink**

- [ ] `context/_partials/followup-sink.md` states the rule once: when the resolved tracker is not `filesystem` and its `auth-check` passes, run `issue-create` from the tracker's reference; otherwise run the `filesystem` reference and report in one line why the tracker was not used. The partial contains no literal `atomic followups add`.
- [ ] The sink applies the `report-issue-privacy` redaction and shows the rendered title and body in the same message as the user's defer or "Later" choice. That choice is the confirmation; there is no second prompt.
- [ ] The sink applies only the severity and kind labels that `label-list` reports as existing, reports every label it skipped, and never creates a label.
- [ ] Every result line the sink prints names the tracker it used. `docs/reference/commands.md` (the `/follow-up` row) and `issue-tracking/filesystem.md` name `issue-tracker: filesystem` as the way to keep follow-ups local. The sink stores no per-repository state.
- [ ] These files compose the sink partial: `loop-finalize` (finalize step), `doc-impact` ("Later"), `/documentation`, `/subagent-diagnose` (`defer`), `/challenge-swarm` (offer 3), `/refresh-wiki` (concerns), and `/follow-up` (record form). The `atomic-documentation` skill does not expand partials and carries one plain sentence pointing to the same rule, without the literal `atomic followups add`.
- [ ] `loop-finalize` dispositions are `fix-now`, `defer`, and `drop`; `defer` records through the sink.
- [ ] A `docs(followups): defer <id>` commit happens only when the sink wrote a file Git tracks. The implementation log records the issue URL or key, or the ledger entry id.
- [ ] `/follow-up <text>` records one follow-up through the sink. `/follow-up review` triages the local ledger; its `promote` step creates the issue through the resolved tracker's reference and closes the entry with `atomic followups close --reason "promoted to <tracker> <id>"`. Bare `/follow-up` prints those two forms.
- [ ] `context/commands/follow-up.md` names `docs/spec/workflow-preferences.md` on its `Spec:` line.
- [ ] A bundle test fails when `atomic followups add` appears in any shipped command, agent, or skill other than `references/issue-tracking/filesystem.md`.

**Reminders removed**

- [ ] `atomic reminder` is an unknown verb: exit 1 with `unknown command "reminder"`, as for any unknown root verb.
- [ ] `/remind-me` does not exist in the bundle or in any install target.
- [ ] `atomic validate` reports no A1 failure, so no shipped artifact cites `atomic reminder`.
- [ ] The help-router verification script in the root `CLAUDE.md` prints zero `MISSING:` lines.
- [ ] The session-start payload contains no reminder text and no `systemMessage` field.
- [ ] `atomic where --json` has no `reminders` key. `config.RemindersDir`, `ProjectRemindersDir`, and `RemindersDirLegacy` are gone, and `scratchpadRelocate` moves no reminder directory.
- [ ] The scratchpad lister still skips a legacy `reminders/` directory by its content rule.
- [ ] `docs/spec/cron-workflow.md` is retired with a `Removed:` change-log entry, and every spec that names reminders in its body is amended.
- [ ] `rg -i remind` over `context/`, `docs/reference/`, `docs/guides/`, `README.md`, the root `CLAUDE.md`, `install.sh`, and `atomic/` returns only these: `atomic/CHANGELOG.md`; the legacy-skip fixture, test, and comment (`atomic/internal/scratchpad/list.go`, `list_test.go`); `<system-reminder>` stripping; the PATH reminder (`install.sh:226`); the TDD "Discipline reminders" heading; the verb "remind" at `context/commands/pressure-test.md:133-134`; the retrospective `Notification` row; `docs/guides/getting-started.md` ("reminds it to update"); the root `CLAUDE.md` rule "Ship verbs must remind the user to run /documentation"; and the `session-start-reminders.sh` legacy script name. `install.sh:223` ("signals / reminders helpers") is edited to drop the word.


## Approach

Resolved preference block from profile, realm, and repo files, delivered at session start with a steering fallback, plus per-provider reference files in the git skill. See `docs/design/workflow-preferences.md`.


## Change tree

```
atomic/
├── internal/
│   ├── workflow/ ........................... A  (WP1, WP3)
│   │   ├── block.go ........................ A  (ParseBlock, refusal rules)
│   │   ├── block_test.go ................... A
│   │   ├── resolve.go ...................... A  (Resolve, precedence, defaults, warnings)
│   │   ├── resolve_test.go ................. A
│   │   ├── origin.go ....................... A  (ProviderForOrigin)
│   │   ├── origin_test.go .................. A
│   │   ├── render.go ....................... A  (RenderText, RenderJSON)
│   │   ├── render_test.go .................. A
│   │   ├── set.go .......................... A  (SetValue, pure transform)
│   │   └── set_test.go ..................... A
│   ├── config/
│   │   ├── harness.go ...................... M  (WP1 workflow-preferences path; WP5b RemindersDir removed)
│   │   ├── harness_test.go ................. M
│   │   ├── projectstate.go ................. M  (WP5b ProjectRemindersDir, RemindersDirLegacy removed)
│   │   └── projectstate_test.go ............ M
│   ├── profile/
│   │   ├── workflow.go ..................... A  (WP2 ApplyWorkflowDetection, DetectWorkflow)
│   │   ├── workflow_test.go ................ A
│   │   ├── detect.go ....................... M  (WP2 registry: glab, tea, fj, acli)
│   │   ├── detect_test.go .................. M
│   │   ├── refresh.go ...................... M  (WP2 detection in Refresh)
│   │   ├── refresh_test.go ................. M
│   │   ├── render.go ....................... M  (WP2 stub section)
│   │   └── render_test.go .................. M
│   ├── hooks/
│   │   ├── hooks.go ........................ M  (WP4 workflow section; reminder section removed)
│   │   └── hooks_test.go ................... M
│   ├── reminder/ ........................... D  (WP5a)
│   │   ├── reminder.go ..................... D
│   │   └── reminder_test.go ................ D
│   ├── cliusage/
│   │   ├── cliusage.go ..................... M  (WP3 workflow rows; WP5a reminder rows removed)
│   │   └── cliusage_test.go ................ M
│   ├── migrate/
│   │   ├── steps_scratchpad.go ............. M  (WP5b reminder half removed)
│   │   └── steps_scratchpad_test.go ........ M
│   ├── serve/api_plans_test.go ............. M  (WP5b comment)
│   ├── embeddedcorpus/embeddedcorpus_test.go  M  (WP10 sink literal test)
│   └── harness/
│       ├── skill_test.go ................... M  (WP7 reference projection and `../` target test, guarding WP8a and WP8b)
│       └── testdata/
│           ├── agent/claude/agents/atomic-reviewer.md ..... M  (WP6a regenerated)
│           ├── agent/omp/agents/atomic-reviewer.md ........ M  (WP6a regenerated)
│           ├── agent/codex/agents/atomic-reviewer.toml .... M  (WP6a regenerated)
│           ├── agent/codex/agents/atomic-reviewer.body.md . M  (WP6a regenerated)
│           ├── skill/claude/skills/atomic-documentation/SKILL.md  M  (WP6a, WP10 regenerated)
│           └── skill/omp/skills/atomic-documentation/SKILL.md ... M  (WP6a, WP10 regenerated)
└── cmd/atomic/
    ├── cmd_workflow.go ..................... A  (WP3 resolve, set)
    ├── cmd_workflow_test.go ................ A
    ├── cmd_where.go ........................ M  (WP5b reminders key removed)
    ├── cmd_where_test.go ................... M
    ├── cmd_reminder.go ..................... D  (WP5a)
    ├── cmd_reminder_test.go ................ D  (WP5a)
    ├── main.go ............................. M  (WP3 workflow; WP5a reminder)
    ├── main_test.go ........................ M
    ├── cmd_scratchpad.go ................... M  (WP5b comment)
    └── testmain_test.go .................... M  (WP5b comment)
context/
├── AGENTS.md ............................... M  (WP6a ~/.atomic row; WP9b steering paragraph, worktree row; WP10 follow-ups row)
├── _partials/
│   ├── followup-sink.md .................... A  (WP10)
│   ├── agent-where.md ...................... M  (WP6a reminders out)
│   ├── doc-impact.md ....................... M  (WP6a Remind out; WP10 sink)
│   ├── loop-finalize.md .................... M  (WP10)
│   ├── base-resolution.md .................. M  (WP9a)
│   ├── pr-flow.md .......................... M  (WP9a)
│   ├── merge-flow.md ....................... M  (WP9a)
│   ├── report-issue-privacy.md ............. M  (WP9a tracker-neutral wording)
│   ├── worktree-setup.md ................... M  (WP9b)
│   └── worktree-cleanup-prompt.md .......... M  (WP9b)
├── commands/
│   ├── remind-me.md ........................ D  (WP6a)
│   ├── follow-up.md ........................ M  (WP6a reminder modes out; WP10 record mode, bare form, Spec line)
│   ├── documentation.md .................... M  (WP6a; WP10)
│   ├── subagent-diagnose.md ................ M  (WP6a; WP9a ci source; WP10)
│   ├── atomic-help.md ...................... M  (WP6a; WP9b in_worktree, base; WP11a)
│   ├── challenge-swarm.md .................. M  (WP10)
│   ├── refresh-wiki.md ..................... M  (WP10)
│   ├── commit.md ........................... M  (WP9a, none behavior)
│   ├── review-branch.md .................... M  (WP9a)
│   ├── git-cleanup.md ...................... M  (WP9a)
│   ├── watch-ci.md ......................... M  (WP9a)
│   ├── report-issue.md ..................... M  (WP9a)
│   ├── autopilot.md ........................ M  (WP9a)
│   └── setup-wiki.md ....................... M  (WP9b)
└── skills/
    ├── atomic-documentation/SKILL.md ....... M  (WP6a; WP10 pointer)
    └── atomic-git-discipline/
        ├── SKILL.md ........................ M  (WP7, WP8a, WP8b index, triggers, screenshots, boundary)
        └── references/ ..................... A
            ├── resolution.md ............... A  (WP7)
            ├── worktrees.md ................ A  (WP8a)
            ├── source-control/ ............. A  (WP7)
            │   ├── github.md ............... A
            │   ├── gitlab.md ............... A
            │   ├── bitbucket.md ............ A
            │   ├── codeberg.md ............. A
            │   └── none.md ................. A
            └── issue-tracking/ ............. A
                ├── github.md ............... A  (WP8a)
                ├── gitlab.md ............... A  (WP8a)
                ├── codeberg.md ............. A  (WP8a)
                ├── filesystem.md ........... A  (WP8a)
                ├── linear.md ............... A  (WP8b)
                ├── jira.md ................. A  (WP8b)
                └── trello.md ............... A  (WP8b)
CLAUDE.md ................................... M  (WP6b hook description)
README.md ................................... M  (WP11a)
install.sh .................................. M  (WP6b install.sh:223)
docs/
├── reference/
│   ├── commands.md ......................... M  (WP6b; WP11a)
│   ├── workflow.md ......................... M  (WP6b; WP11a)
│   ├── concepts.md ......................... M  (WP6b; WP11a)
│   ├── conventions.md ...................... M  (WP6b; WP11a)
│   ├── atomic-toml.md ...................... M  (WP6b)
│   └── skills.md ........................... M  (WP11a)
├── guides/
│   ├── install.md .......................... M  (WP6b; WP11a)
│   ├── contributing.md ..................... M  (WP6b)
│   └── getting-started.md .................. M  (WP11a)
├── credits.md .............................. M  (WP6b)
├── spec/
│   ├── workflow-preferences.md ............. A  (this spec)
│   ├── user-profile.md ..................... M  (WP2 schema)
│   ├── atomic-where.md ..................... M  (WP6c reminders key removed)
│   ├── cron-workflow.md .................... M  (WP6c retired)
│   ├── atomic-binary.md .................... M  (WP6c)
│   ├── serve-plans-page.md ................. M  (WP6c)
│   ├── atomic-state-and-config.md .......... M  (WP6c; WP11b)
│   ├── configurable-state-paths.md ......... M  (WP6c; WP11b)
│   ├── realm-wiki-drop-refresh.md .......... M  (WP6c)
│   ├── cli-trailing-flags.md ............... M  (WP6d)
│   ├── cli-cobra.md ........................ M  (WP6d)
│   ├── validate-artifact-cli-flags.md ...... M  (WP6d body flag example)
│   ├── documentation-as-maintenance.md ..... M  (WP6d; WP11b)
│   ├── typed-followups.md .................. M  (WP6d; WP11b)
│   ├── repo-init.md ........................ M  (WP6d)
│   ├── follow-ups-folder.md ................ M  (WP11b)
│   └── subagent-diagnose.md ................ M  (WP11b)
└── wiki/*.md ............................... M  (WP11b regenerated by /refresh-wiki)
```


## Outline

```
atomic/internal/workflow/block.go
  Block — one parsed preference block
    Values — key to value, note, and soft flag
    Warnings — unknown keys, invalid values, repeated keys, level mismatch
  ParseBlock — locate and parse one block in file content, refusing duplicates and unpaired tags
  Keys — the five keys and their allowed values

atomic/internal/workflow/resolve.go
  Level — repo, realm, profile, origin, derived, default
  Resolved — per-key value and level, warnings, reference paths
  Resolve — read the three levels for a directory and apply the precedence rule
  defaultFor — built-in default per key
  trackerFor — tracker of a source-control provider, or filesystem, at level derived

atomic/internal/workflow/origin.go
  ProviderForOrigin — parse an origin URL into its host and mapped provider

atomic/internal/workflow/render.go
  RenderText — the `level='resolved'` block
  RenderJSON — the same facts as JSON

atomic/internal/workflow/set.go
  SetValue — write one explicit line into a block, creating section or block when absent

atomic/internal/config/harness.go
  WorkflowPrefsPath — `<state-root>/workflow-preferences.md` for a root
  RemindersDir — removed

atomic/internal/config/projectstate.go
  ProjectRemindersDir — removed
  RemindersDirLegacy — removed

atomic/internal/profile/workflow.go
  DetectWorkflow — authenticated source-control providers and non-host tracker credentials
  ApplyWorkflowDetection — rewrite absent or detected profile lines, preserving user bytes

atomic/internal/profile/detect.go
  DefaultRegistry — gains glab, tea, fj, acli

atomic/internal/profile/refresh.go
  Refresh — runs ApplyWorkflowDetection before the Environment rewrite

atomic/internal/profile/render.go
  writeStubSections — emits `## Workflow preferences` with an empty block before `## Environment`

atomic/internal/hooks/hooks.go
  buildBody — section list: orientation, then workflow preferences
  SessionStart — no reminder read, no systemMessage
  reminder helpers — removed

atomic/cmd/atomic/cmd_workflow.go
  buildWorkflowCmd — `workflow` verb family
    resolve — print the resolved block
    set — write one preference line

atomic/cmd/atomic/cmd_where.go
  where JSON — loses reminders

atomic/cmd/atomic/main.go
  root verbs — add workflow, drop reminder

atomic/internal/cliusage/cliusage.go
  rows — workflow resolve, workflow set; reminder rows removed

atomic/internal/migrate/steps_scratchpad.go
  scratchpadRelocate — reminder relocation removed

atomic/internal/embeddedcorpus/embeddedcorpus_test.go
  sink literal — `atomic followups add` only in filesystem.md

atomic/internal/harness/skill_test.go
  git skill references — every reference projects at its nested path
  relative reference targets — every `../` target in references/** ships

context/AGENTS.md
  Workflow preferences — the verbatim steering paragraph
  Where things live — worktrees, follow-ups, and ~/.atomic rows

context/_partials/followup-sink.md
  followup-sink — redact and preview, tracker issue with existing labels, or the filesystem reference

context/_partials/agent-where.md
  field list — reminders removed

context/_partials/loop-finalize.md
  Finalize dispositions — fix-now, defer through the sink, drop

context/_partials/doc-impact.md
  Later — through the sink; Remind removed

context/_partials/base-resolution.md
  default branch — symbolic-ref, git config, then the reference row

context/_partials/pr-flow.md
  change request — resolved provider line, then reference operations

context/_partials/merge-flow.md
  merge — resolved provider line, reference operations, none takes the local path

context/_partials/report-issue-privacy.md
  privacy — tracker-neutral wording; redact, preview, confirm only

context/_partials/worktree-setup.md
  placement — worktrees field and worktrees reference

context/_partials/worktree-cleanup-prompt.md
  enumeration — git worktree list, no path assumption

context/commands/follow-up.md
  bare — print the two forms
  record — one follow-up through the sink
  review — local triage
    promote — issue-create through the resolved tracker

context/commands/remind-me.md
  removed

context/commands/documentation.md
  Later — through the sink; Remind removed

context/commands/subagent-diagnose.md
  ci source — watch-ci provider detection
  defer — through the sink; reminder chain removed

context/commands/challenge-swarm.md
  offer 3 — through the sink

context/commands/refresh-wiki.md
  concerns — through the sink

context/commands/commit.md
  pr path — change request through the reference
  none — push and pr refuse; merge and squash merge go local

context/commands/review-branch.md
  base — composes base-resolution

context/commands/git-cleanup.md
  base — composes base-resolution
  scan — git worktree list

context/commands/watch-ci.md
  target — ci-target-resolve operation

context/commands/report-issue.md
  flow — resolved tracker line, then reference operations

context/commands/autopilot.md
  issue input — issue-view operation

context/commands/setup-wiki.md
  ignore rule — resolved worktrees location

context/commands/atomic-help.md
  in_worktree — git-dir test
  base — composes base-resolution
  topics — workflow verbs, worktrees, no reminders
  tour stage 3 — workflow-preferences.md, no reminders

context/skills/atomic-documentation/SKILL.md
  Later — sink pointer sentence; Remind removed

context/skills/atomic-git-discipline/SKILL.md
  description — names its invokers
  trigger — widened, no provider CLI
  References — index of every reference path
  Screenshots — points to change-request-attach-images
  Boundaries — provider-neutral

context/skills/atomic-git-discipline/references/resolution.md
  block to file mapping — which file each resolved value selects

context/skills/atomic-git-discipline/references/worktrees.md
  placement per harness — Claude Code, OMP, Codex, explicit key
  create and enter — per harness
  clean — git worktree list

context/skills/atomic-git-discipline/references/source-control/<provider>.md
  operations — the eleven source-control rows, each with a vendor URL or unverified
  gotchas — tokens, scopes, environment, rate limits

context/skills/atomic-git-discipline/references/issue-tracking/<tracker>.md
  operations — the nine issue-tracking rows, each with a vendor URL or unverified
  gotchas — tokens, scopes, environment, rate limits

docs/spec/user-profile.md
  Schema contract — seven sections, Workflow preferences placement
  Append contract — Workflow preferences is not a Claude append target

docs/spec/atomic-where.md
  JSON fields — reminders removed

docs/spec/cron-workflow.md
  retired — Removed entry
```


## Flows

**Flow: preference resolution**

1. A caller runs `atomic workflow resolve --cwd DIR` (or the session-start builder calls `workflow.Resolve`).
2. Resolver finds the repository root and resolved state root for `DIR`, the realm root if `DIR` sits in a registered realm, and `~/.atomic/profile.md`. With no repository or an ambiguous state root, it skips the repo level and records a warning.
3. Resolver parses the block at each level. A refusal case skips that level and records a warning.
4. Resolver reads `git remote get-url origin` and maps the host with `ProviderForOrigin`.
5. For each key, resolver applies the precedence: explicit value; origin for `source-control` and `source-control-host`; for `issue-tracker`, the soft non-host tracker, else the tracker derived from the resolved `source-control` (level `derived`); the soft value for every other key; the default.
6. Resolver maps `worktrees: auto` through the worktree directory helper for the harness and adds the mismatch and unknown-host warnings.
7. Renderer prints the `level='resolved'` block with the set keys and their levels, warnings, and the `Read:` line. The verb exits 0.

**Flow: autodetect refresh**

1. Session start, install, update, or `atomic profile refresh` calls `profile.Refresh` when the profile is stale.
2. `DetectWorkflow` reads each provider CLI's host configuration file and checks credential variable names.
3. `ApplyWorkflowDetection` locates the profile block. A duplicate, unpaired, or misplaced block stops the rewrite, and refresh prints the reason.
4. With no section, it inserts `## Workflow preferences` and the block before `## Environment`.
5. With exactly one authenticated source-control provider, it writes or updates `source-control` when the line is absent or carries `# detected`; with two or more it removes a `# detected` line. It treats non-host tracker credentials and `issue-tracker` the same way.
6. Refresh runs the Environment rewrite and writes the file atomically only when the bytes changed.

**Flow: session carrier delivery**

1. Claude Code fires `SessionStart` and runs `atomic hooks session-start --harness claude`; the verb takes the directory from `--cwd`, stdin `cwd`, or the process directory, as CP9A of `docs/spec/omp-plugin-compatibility.md` defines.
2. `buildBody` resolves orientation, then calls `workflow.Resolve` for the session directory and harness.
3. `buildBody` returns its section list, `## Orientation` then `## Workflow preferences`, delivered in `additionalContext`. On resolver failure it omits the second section and exits 0.
4. OMP and Codex run the same verb through the carriers in `docs/spec/omp-plugin-compatibility.md`; CP13A consumes the same section list and adds none.
5. When no resolved block reaches context, the model follows the steering paragraph, runs `atomic workflow resolve` once, and uses its output.
6. Before a provider operation, the model reads the references named on the `Read:` line.

**Flow: follow-up recording**

1. A command reaches a follow-up point: finalize `defer`, doc-impact "Later", `/documentation` "Later", `/subagent-diagnose` `defer`, `/challenge-swarm` offer 3, `/refresh-wiki` concerns, or `/follow-up <text>`.
2. The expanded `followup-sink` text redacts the body per `report-issue-privacy` and shows the rendered title and body alongside the user's defer choice.
3. When the resolved tracker is not `filesystem`, the model runs the tracker reference's `auth-check`. On success it runs `label-list`, then `issue-create` with title, body (origin, file, finding), and the existing severity and kind labels, and reports the tracker used, the issue URL or key, and any skipped label.
4. When the tracker is `filesystem` or the auth check fails, the model runs the `filesystem` reference and reports why.
5. `loop-finalize` commits `docs(followups): defer <id>` only when the ledger file is tracked by Git, and records the URL, key, or entry id in the implementation log.
6. Later, `/follow-up review` lets the user `promote` a ledger entry: issue-create through the resolved tracker, then `atomic followups close --reason "promoted to <tracker> <id>"`.

**Flow: worktree placement**

1. `worktree-setup` decides a new worktree is needed.
2. The model reads the `worktrees` field of `atomic where --json` and `references/worktrees.md`.
3. On Claude Code with the default, the model enters the worktree through `EnterWorktree` or `claude --worktree`, and Claude Code creates `.claude/worktrees/<branch>`.
4. On OMP or Codex, or with an explicit `worktrees` path, the model runs `git worktree add <dir>/<branch> -b <branch>` and changes into it.
5. With `worktrees: off`, the model works in the current checkout.
6. Cleanup and `/git-cleanup` enumerate worktrees with `git worktree list` and prompt for deletion.

**Flow: reminder removal**

1. A user who types `atomic reminder` gets the unknown-verb error and exit 1.
2. `/remind-me` is absent from the slash menu, the bundle, and `/atomic-help`.
3. Session start emits orientation and workflow preferences only; reminder files under `~/.atomic/<project-key>/reminders/` or a legacy scratchpad stay on disk and nothing reads them.
4. `scratchpadRelocate` leaves any reminder directory where it is.
5. `atomic where --json` consumers read the remaining fields; `reminders` is absent.


## Checkpoints


Cross-spec order: the "Execution order" table in `docs/spec/omp-plugin-compatibility.md` sequences these rows with that spec's checkpoints.

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| WP1 | Block parser, resolver, origin-host inference, warnings, renderer, workflow-preferences path. Depends on CP9A, CP10B; consumes the CP10B worktree helper. CP9B2 follows WP1 | `atomic/internal/workflow/{block,resolve,origin,render}.go` + tests, `config/harness.go`, `harness_test.go` | atomic-implementer (mode: feature) | ~10 | `go test ./internal/workflow ./internal/config`: grammar, refusal, missing and mismatched `level`, precedence, the Bitbucket row, `source-control-host` from origin, every warning, the soft-host-tracker warning, the no-origin soft row, the outside-repository row, `derived` level, unset keys omitted, `auto` through the worktree helper |
| WP2 | Profile autodetect and stub section. Depends on WP1 | `profile/{workflow,detect,refresh,render}.go` + tests, `docs/spec/user-profile.md` | atomic-implementer (mode: feature) | ~9 | `go test ./internal/profile`: profile-only writes, user lines byte-preserved, idempotence, refusal cases, placement before `## Environment`, the four detection test rows; no token value read |
| WP3 | `atomic workflow resolve` and `set`, cliusage rows, root verb table. Depends on WP1, CP9A, CP10B. Builds no migration trigger; CP11 wires it and depends on WP3 | `cmd/atomic/cmd_workflow.go` + test, `workflow/set.go` + test, `main.go`, `main_test.go`, `cliusage.go`, `cliusage_test.go` | atomic-implementer (mode: feature) | ~8 | `go test ./cmd/atomic ./internal/workflow ./internal/cliusage`; exit codes 0/1/2; the built binary under a sandboxed `HOME` for `resolve`, `resolve --json` (parses as JSON), and `set` at each level; `resolve` outside a repository exits 0 with a warning |
| WP4 | `buildBody` section list (orientation, then `## Workflow preferences`); reminder section removed from the builder. Depends on CP9A (owns `--harness`, `--cwd`, stdin) and WP1; lands before CP13A, which consumes the section list | `hooks/hooks.go`, `hooks_test.go` | atomic-implementer (mode: surgical) | 2 | `go test ./internal/hooks`: section present after orientation, fail-open on resolver error, no reminder text, no `systemMessage` |
| WP5a | Reminder removal, Go: package, verb, cliusage, root verbs. Lands after WP4, because `hooks.go` and its tests import the reminder package until then | `internal/reminder/` (D), `cmd_reminder.go` + test (D), `main.go`, `main_test.go`, `cliusage.go`, `cliusage_test.go` | atomic-implementer (mode: feature) | ~8 | `go test ./...`; `atomic reminder` exits 1 with `unknown command "reminder"`; root verb count test passes. A1 lint is checked at WP6a |
| WP5b | Reminder removal, Go: config helpers, `where --json` key, migrate half, stale comments. Lands after WP5a | `config/{harness,projectstate}.go` + tests, `cmd_where.go` + test, `migrate/steps_scratchpad.go` + test, `cmd_scratchpad.go`, `testmain_test.go`, `serve/api_plans_test.go` | atomic-implementer (mode: feature) | ~12 | `go test ./...`; `rg -i remind` over non-testdata Go sources under `atomic/` shows only the exceptions listed in the success criteria; scratchpad lister skip test still passes |
| WP6a | Reminder removal, corpus. Runs before CP10D2-D3 and CP15A3-A4, which share these files | `commands/remind-me.md` (D), `follow-up.md`, `_partials/doc-impact.md`, `documentation.md`, `atomic-documentation/SKILL.md`, `_partials/agent-where.md`, `subagent-diagnose.md`, `atomic-help.md`, `AGENTS.md`, harness goldens | atomic-implementer (mode: feature) | ~9 + 6 goldens | `make -C atomic bundle`; `go test ./internal/harness -update` then `go test ./...`; `atomic validate` A1 clean; help-router script zero `MISSING:` (checklist row 7); `rg -i remind` over `context/` and `atomic/` shows only the listed exceptions |
| WP6b | Reminder removal, docs and guides | `docs/reference/{commands,workflow,concepts,conventions,atomic-toml}.md`, `docs/guides/{install,contributing}.md`, `docs/credits.md`, root `CLAUDE.md`, `install.sh` | atomic-implementer (mode: feature) | ~10 | the full `rg -i remind` sweep from the success criteria (including the root `CLAUDE.md` and `install.sh`) shows only the listed exceptions; `docs/reference/commands.md` table has no `/remind-me` row (checklist row 4); `npm run docs:build` passes |
| WP6c | Reminder removal, core specs | `docs/spec/{cron-workflow,atomic-binary,atomic-where,serve-plans-page,atomic-state-and-config,configurable-state-paths,realm-wiki-drop-refresh}.md` | atomic-implementer (mode: feature) | ~7 | `atomic validate spec` 0 FAIL; `cron-workflow.md` carries a `Removed:` entry; each amended body names no reminder behavior |
| WP6d | Reminder removal, remaining specs | `docs/spec/{cli-trailing-flags,cli-cobra,validate-artifact-cli-flags,documentation-as-maintenance,typed-followups,repo-init}.md` | atomic-implementer (mode: feature) | ~6 | `atomic validate spec` 0 FAIL; change-log entries added where a body changed |
| WP7 | Git-skill source-control references, `resolution.md`, SKILL.md index, triggers, screenshots, boundary, description | `atomic-git-discipline/SKILL.md`, `references/resolution.md`, `references/source-control/{github,gitlab,bitbucket,codeberg,none}.md`, `harness/skill_test.go` | atomic-implementer (mode: feature) | ~8 | a grep for each of the eleven operation ids finds it in all five files; every operation row carries a vendor URL or `unverified`; SKILL.md names no provider CLI in trigger, screenshots, or boundary; the description names its invokers (checklist row 6); `go test ./internal/harness ./internal/harness/omp ./internal/harness/codex` shows each reference projected and every declared path resolved; the `../` target test passes; no file under `references/` names a `references/...` path; `atomic validate artifacts` 0 FAIL |
| WP8a | Host and filesystem issue-tracking references and `worktrees.md`. Depends on WP7 (the `../source-control/` files and the `../` target test) | `references/issue-tracking/{github,gitlab,codeberg,filesystem}.md`, `references/worktrees.md`, `SKILL.md` | atomic-implementer (mode: feature) | ~7 | a grep for each of the nine operation ids finds it in all four files; every operation row carries a vendor URL or `unverified`; `filesystem.md` names no folder; host trackers point to `../source-control/<provider>.md`; the description names its invokers (checklist row 6); `go test ./internal/harness ./internal/harness/omp ./internal/harness/codex` covers the new files and resolves every declared path; the `../` target test passes; `atomic validate artifacts` 0 FAIL |
| WP8b | Non-host issue-tracking references. Depends on WP7 (the `../` target test) | `references/issue-tracking/{linear,jira,trello}.md`, `SKILL.md` | atomic-implementer (mode: feature) | ~5 | a grep for each of the nine operation ids finds it in all three files; every operation row carries a vendor URL or `unverified`; the description names its invokers (checklist row 6); `go test ./internal/harness ./internal/harness/omp ./internal/harness/codex` covers the new files and resolves every declared path; the `../` target test passes; `atomic validate artifacts` 0 FAIL |
| WP9a | Wire ship, review, CI, diagnosis, and issue commands to the references. Runs before CP10D2-D3 and CP15A3-A4 | `_partials/{base-resolution,pr-flow,merge-flow,report-issue-privacy}.md`, `commands/{commit,review-branch,git-cleanup,watch-ci,report-issue,autopilot,subagent-diagnose}.md` | atomic-implementer (mode: feature) | ~11 | `make -C atomic bundle`; `atomic validate artifacts` 0 FAIL; no edited file names a `gh pr`, `gh issue`, `gh repo`, or `gh run` command; resolved-provider line present before the first provider operation; `none` refusal and local-merge paths present in `commit` and `merge-flow`; `report-issue-privacy` names no provider operation |
| WP9b | Wire worktree flows and the steering paragraph. Depends on CP9B2 (the `worktrees` field, consuming WP1's preference) and CP10B (`atomic repo init` ignore rules); runs before CP10D2-D3 and CP15A3-A4 | `_partials/{worktree-setup,worktree-cleanup-prompt}.md`, `commands/{atomic-help,setup-wiki}.md`, `context/AGENTS.md` | atomic-implementer (mode: feature) | ~5 | steering paragraph matches the verbatim contract byte for byte; `atomic-help.md` names no `gh repo view` or other `gh` command; `setup-wiki.md` reports the exact `.gitignore` line for an explicit `worktrees` path; `go test ./internal/artifacts ./internal/harness`; help-router script zero `MISSING:` (checklist row 7); `atomic validate artifacts` 0 FAIL |
| WP10 | Follow-up sink partial, its call sites, `/follow-up` forms and `Spec:` line, skill pointer, disposition merge, sink literal test. Depends on WP8a (filesystem reference); runs before CP10D2-D3 and CP15A3-A4 | `_partials/{followup-sink,loop-finalize,doc-impact}.md`, `commands/{documentation,subagent-diagnose,challenge-swarm,refresh-wiki,follow-up}.md`, `atomic-documentation/SKILL.md`, `AGENTS.md`, `embeddedcorpus_test.go`, harness goldens | atomic-implementer (mode: feature) | ~12 | `make -C atomic bundle`; `go test ./internal/embeddedcorpus ./internal/harness`; sink literal test fails on a planted literal and passes after; `follow-up.md` (promote flow included) names no `gh` command; the partial carries the redaction, preview, existing-label, and tracker-named-in-result rules; `atomic validate artifacts` 0 FAIL |
| WP11a | Docs, help, and README sweep | `docs/reference/{concepts,workflow,commands,skills,conventions}.md`, `docs/guides/{getting-started,install}.md`, `README.md`, `commands/atomic-help.md` | atomic-implementer (mode: feature) | ~9 | checklist rows 4 and 7: `/atomic-help` final pass reaches `atomic workflow resolve` and `set`, tour stage 3 names `<state-root>/workflow-preferences.md`, help-router script zero `MISSING:`, README and `docs/reference/` tables current; `npm run docs:build` passes |
| WP11b | Spec amendments for the sink and preferences, then `/refresh-wiki` | `docs/spec/{follow-ups-folder,typed-followups,documentation-as-maintenance,subagent-diagnose,atomic-state-and-config,configurable-state-paths}.md`, `docs/wiki/*.md` | atomic-implementer (mode: feature) | ~6 + wiki | `atomic validate spec` 0 FAIL; the `follow-ups-folder`, `typed-followups`, `documentation-as-maintenance`, and `subagent-diagnose` bodies state the sink behavior for `defer`, "Later", and `/follow-up`; the other two bodies name the workflow-preferences file; checklist row 8 needs no mirror change (`go test ./internal/bundlemirror` passes); checklist row 9: `/refresh-wiki` run and committed as `chore(signals)` |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Default behavior change: a GitHub user with `gh` signed in now gets remote issues from `defer` and "Later" | high | Every sink result line names the tracker used, `issue-tracker: filesystem` is documented as the way back, and the rendered issue is shown with the user's choice before it is created |
| Wrong-provider side effects: a misresolved provider opens, merges, or files in the wrong place | med | Detection writes only on one unambiguous provider; origin outranks soft values; explicit-versus-origin mismatch warns; ship verbs and `/report-issue` print the resolved provider and level before the first provider operation; destructive confirmations unchanged |
| Reference accuracy: CLI flags, CLI host-config file locations, and REST shapes drift or were never verified | high | Every operation row cites a vendor URL or says `unverified`; no provider ships without its check; Bitbucket's issue-tracker retirement is confirmed from an Atlassian source before `bitbucket.md` ships |
| Model non-adherence: the model ignores the block or the reference | med | The steering paragraph is an LLM safeguard layer over the hook; each consuming partial carries one explicit read-the-reference sentence, because subagents do not auto-fire skills |
| Codex hook trust: the `SessionStart` hook stays silent until the user trusts it, and Codex receives steering only once its native instruction materialization lands | high | The steering paragraph is the Codex carrier; the gap is reported as `unsupported` in the surfaces `docs/spec/omp-plugin-compatibility.md` defines |
| Hybrid hand-edited block: a user edit leaves duplicate, unpaired, or misplaced tags | med | Every such case is a refusal that writes nothing and names the reason; user lines are never rewritten; deleting `# detected` pins a value |
| Skill scope drift and preload token cost: three agents preload `atomic-git-discipline` | med | The SKILL.md index stays short and references load on demand; the boundary line covers message format plus where work is placed, and nothing more |
| Public issue leaks private detail | med | The sink applies the `report-issue-privacy` redaction and shows the rendered issue before creation |
| Existing committed follow-up entries and wiki links to them | low | The ledger and its folder are unchanged, so entries and links keep working; `/follow-up review` `promote` moves one entry on request |
| `atomic where --json` shape change: `reminders` disappears | low | Every shipped consumer is edited in WP6a |
| Release classification: removing `/remind-me` and `atomic reminder` is breaking, and a prose squash title ships the change invisibly | med | The squash-merge title is `feat!:` |


## Change log

<!-- Populated on first amendment after the spec is approved. Do not log drafting/refinement turns. -->
