---
name: atomic-verify
description: Verify completion claims with fresh evidence. Require independent review for code written directly outside an implementation loop.
---
Run the narrowest check that proves the claim. Read its output and exit status before saying what passed. If the check fails or cannot run, report that and narrow the claim.

| Claim | Evidence |
|---|---|
| Changed behavior works | Run the scenario or targeted test that exercises it. |
| A bug is fixed | Use the original repro or a regression test. Establish that it failed for the expected reason before the fix, then run it against the fix. |
| The full suite, build, or lint passes | Run the corresponding full command. A targeted result does not cover the rest. |
| Delegated work is complete | Inspect the delivered changes and verify the requested outcome. |
| Specs or bundled prompts are valid | Run `atomic validate spec` for changed specs, `atomic validate config`, and `atomic validate artifacts` for applicable changes. Report any check you could not run. |

For code you wrote directly outside an implementation loop, dispatch a fresh `atomic-reviewer` in code mode with `diff: working` and the task intent. If the tree contains unrelated changes, use `diff: patch <path>` instead and include task-owned new files in the patch. Use the session model when the harness supports an override. Resolve blockers before claiming completion. Report `review: <verdict> (atomic-reviewer on <actual model>)`.

Intent and progress reports can state what you plan to do or what you observed without claiming completion.
