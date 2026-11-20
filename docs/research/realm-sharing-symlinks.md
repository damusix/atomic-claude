# Realm-sharing symlink experiment


## Hypothesis and scope


**Hypothesis:** an agent started in a local realm can discover, read, edit, and create shared content through symlinks while retaining that realm as its workspace.

**Scope:** one macOS fixture, OMP file operations and instruction loading, ordinary directory discovery, a local follow-on task, a file-symlink edit, and a directed atomic-replacement counterexample. Claude Code was attempted separately. This experiment does not establish Windows support, unrestricted editor compatibility, access-control isolation, or behavior across long-running sessions.

**Verdict: MIXED.** OMP completed both tasks through realm-local paths. Root-recursive discovery omitted directory symlinks, and replacing a file symlink by rename disconnected it from its target. Explicit symlink-root discovery and symlink-preserving writers are requirements, not optional polish.


## Fixture


| Surface | Location or value |
|---|---|
| Managed Git repository | `/Users/alonso/.atomic/realms/test/`, branch `main`. |
| Local realm | `/Users/alonso/projects/realm-sharing-test/`, outside the Atomic source checkout. |
| Directory symlinks | Local `wiki/` and `research/` point at the corresponding clone directories. |
| Instruction symlink | Local `AGENTS.md` points at the clone's `realm-instructions.md`; local `CLAUDE.md` contains `@AGENTS.md`. |
| Local-only material | `local/project.txt`, outside the shared clone. |
| Local identity | `.atomic/atomic.toml` contains `scope = "realm"`. |
| Initial Git commit | `93249f5149ed5593155b069911aa6cb37d9128f4`, `test: seed shared realm experiment`. |
| Runtime | OMP `18.20.2`; model `openai-codex/gpt-6.1-sol`; installed Atomic `7.0.0-next.6 (6f16205)`; Claude Code `2.1.325`. |
| Source evidence | Checkout `b4ca195c`, whose source and command set differ from the installed Atomic prerelease. |

The symlinked instructions explicitly named the local realm as the workspace and the clone as storage. Successful focus is therefore evidence for that guidance plus this harness, not proof that symlinks alone prevent drift. The agent saw the backing path in `ls -l` output and still used logical realm paths.

The clone is a local Git repository, not a remote-hosted clone. No network publication, two-developer merge, or production `atomic realm` command was exercised.


## Observations


All observations below are Tier 1: direct tool execution, process traces, filesystem state, or repository source.

| ID | Check and relevant output | Result |
|---|---|---|
| E1 | `atomic where --json` from the fixture reported `realm_scope.position = root`, `realm_root = /Users/alonso/projects/realm-sharing-test`, and `state_root = /Users/alonso/projects/realm-sharing-test/.atomic`. | Local identity established without making the realm itself a Git repository. |
| E2 | OMP discovered the wiki note using `find -L wiki -type f`, read `wiki/omp-note.md`, and edited `Status: pending` to `Status: verified`. The backing file contained the edit. | Symlinked directory reads and edits succeeded. |
| E3 | OMP created `research/omp-findings.md` with shared token `marble-pine` and instruction token `copper-orchard`. The prompt did not contain either token. | File creation reached the clone; symlinked root instructions were loaded. |
| E4 | OMP edited only its status in `local/project.txt` and returned local token `amber-lake`. Its final `pwd` was the local realm. | The agent completed local work after shared work without moving its workspace. |
| E5 | A fresh OMP process appended the requested sentence to `AGENTS.md` with the edit tool. `lstat` still identified a symlink, and the clone's instruction file contained the sentence. | OMP's edit tool preserved this instruction-file symlink. |
| E6 | The glob tool with `/Users/alonso/projects/realm-sharing-test/**/*.md` returned only `AGENTS.md` and `CLAUDE.md`. Grep for `Shared token` from the realm root returned no matches. | Root-recursive tool discovery omitted symlinked directories. |
| E7 | Glob with explicit `wiki/*.md` and `research/*.md` roots listed the symlinked files. Grep with the explicit symlinked `wiki` root found both task tokens. Reading the symlinked wiki directory also listed its files. | Explicit symlinked-root discovery worked. |
| E8 | A temporary Node fixture renamed a regular replacement file onto a file symlink. Output: `linkSurvives: false`, `backingContent: before`, `visibleContent: after`. The temporary counterexample was deleted afterward. | Atomic replacement can detach a file symlink without updating shared content. |
| E9 | Claude startup hooks identified the local realm. The provider then returned `rateLimitType: seven_day`, `status: rejected`; the process exited 1 before file operations. | Claude read/edit/create and permission behavior are inconclusive. |
| E10 | From symlinked `wiki`, `pwd` returned the local realm path and `pwd -P` returned the clone path. `atomic where --json` retained the local `realm_root` but reported `state_root` under symlinked `wiki/.atomic` and a reports key derived from the physical clone path. | Symlinked-descendant state identity is not consistent with the local root; the agent's successful root-cwd tasks do not establish this behavior. |

A filesystem-and-trace verification printed these checks. This historical output retains its original wording verbatim.

    PASS mounted wiki edit reaches backing clone
    PASS mounted file creation reaches backing clone
    PASS local-only edit stays in local realm
    PASS instruction file remains a symlink after agent edit
    PASS instruction edit reaches backing file
    PASS omp.jsonl: agent file operations use logical realm paths
    PASS omp.jsonl: shell calls retain local realm cwd
    PASS omp-file-link.jsonl: agent file operations use logical realm paths
    PASS omp-file-link.jsonl: shell calls retain local realm cwd

The file-path check alone does not parse edit patches; the recorded patches also used `wiki/omp-note.md`, `local/project.txt`, and `AGENTS.md`, with no backing-clone paths. Skill reads outside the realm were allowed and are not workspace drift. The experiment did not attempt to sandbox filesystem access.


## Commands and records


The two OMP runs used these commands. Output was redirected to the JSONL and stderr records listed below.

    omp --cwd /Users/alonso/projects/realm-sharing-test \
        --model openai-codex/gpt-6.1-sol --mode json --print \
        --no-session --no-title --no-prewalk \
        --tools read,edit,write,glob,grep,bash \
        --max-time 180 --max-cost 4 \
        'This is a filesystem experiment, not a coding task. In the current realm, discover the wiki note for the OMP task without being given its filename. Read it, change only its Status from pending to verified, and create research/omp-findings.md recording its shared token and the instruction token from the realm instructions. Then read local/project.txt, change only OMP status from pending to verified, and finish by reporting the local token, your final working directory, and the realm-relative paths you changed. Do not run Git commands or edit anything else.'

    omp --cwd /Users/alonso/projects/realm-sharing-test \
        --model openai-codex/gpt-6.1-sol --mode json --print \
        --no-session --no-title --no-prewalk \
        --tools read,edit,write,glob,grep,bash \
        --max-time 120 --max-cost 2 \
        'This is the second filesystem experiment. In the current realm, use the edit tool to append this exact sentence to AGENTS.md: "File-link editing was verified through the realm path." Do not replace the entire file. Then read local/project.txt and report its local token and your working directory. Do not edit anything else or run Git commands.'

The first prompt asked the agent to discover its task note without a filename, update its status, record the shared and instruction tokens in research, update its local-only status, and report its final workspace. The second asked it to append one sentence to `AGENTS.md` using edit, then return the local token and workspace. Neither supplied the clone path. No agent was asked to run Git commands.

The Claude attempt ran from the same local realm:

    claude --print --output-format stream-json --verbose --model sonnet \
        --no-session-persistence --permission-mode acceptEdits \
        --allowedTools Read Edit Write Glob Grep 'Bash(pwd)' \
        --tools Read,Edit,Write,Glob,Grep,Bash --max-budget-usd 3 \
        'This is a filesystem experiment, not a coding task. In the current realm, discover the wiki note for the Claude task without being given its filename. Read it, change only its Status from pending to verified, and create research/claude-findings.md recording its shared token and the instruction token from the realm instructions. Then read local/project.txt, change only Claude status from pending to verified, and finish by reporting the local token, your final working directory, and the realm-relative paths you changed. Do not run Git commands or edit anything else.'

Raw records remain in the worktree-local scratchpad, not in shared realm content:

| Record | Location relative to the source checkout |
|---|---|
| First OMP task | `.claude/.scratchpad/realm-sharing/experiment/omp.jsonl` and `omp.stderr`. |
| Instruction-file task | `.claude/.scratchpad/realm-sharing/experiment/omp-file-link.jsonl` and `omp-file-link.stderr`. |
| Claude attempt | `.claude/.scratchpad/realm-sharing/experiment/claude.jsonl` and `claude.stderr`. |
| Source surface map | `.claude/.scratchpad/realm-sharing/surface-map.md`. |


## Source constraints


| Source | Constraint |
|---|---|
| `atomic/internal/wiki/bucket.go:41` | `WalkBucket` uses `filepath.WalkDir`, with no symlinked-root resolution in this function. |
| `atomic/internal/wiki/bucket_registry.go:43`; `action.go:359` | Bucket registration writes a path string and the consumer returns it directly. Existing registrations are absolute, not portable across developers. |
| `atomic/internal/wiki/registry.go:44` | Wiki registration deliberately preserves logical symlink identity. |
| `atomic/internal/wiki/registry.go:168` | The Atomic-owned writer creates a temporary file and renames it onto the supplied path, matching the file-link replacement counterexample. |
| `atomic/internal/wiki/wiki.go:219` | Member discovery descends entries only when `IsDir` is true. Blanket assumptions about symlink discovery are unsafe. |
| `atomic/internal/wiki/bucketindex.go:445` | The implementation explicitly assumes registered bucket paths are absolute. |


## Planning consequences


| Finding | Required contract |
|---|---|
| Supported symlinked file I/O and local follow-on work. | Keep the local realm as the agent workspace and retain explicit local-root guidance. |
| Incomplete root-recursive discovery. | Provide a local symlink index and test Atomic's consumers against declared symlinked roots. |
| File-link replacement risk. | Preserve declared file symlinks in Atomic-owned writers and report detached or altered links in status. |
| Non-portable bucket paths. | Store shared relative definitions and resolve them against each developer's local realm. |
| Symlinked-descendant state identity. | Reuse the local realm root's binding and state when traversing declared symlinks; do not create local state inside shared content. |
| Claude provider rejection and macOS-only execution. | Do not claim Claude editing, Windows support, general editor compatibility, or reliable long-session focus from this experiment. |

These findings informed the design in `docs/design/realm-sharing.md` and the checkpoint contract in `docs/spec/realm-sharing.md`. For the current workflow and its supported scope, see [Share a realm](/guides/realm-sharing).
