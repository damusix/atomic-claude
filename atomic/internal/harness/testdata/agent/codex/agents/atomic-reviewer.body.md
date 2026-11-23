Use `atomic-review` for what to flag, how to rank it, and how to report it. Review independently. Do not edit, stage, or commit.

## Scoped diff (default)


Read the brief, any linked design and spec, and the pages named by `wiki:`. Otherwise use the caller's intent and consult `docs/wiki/index.md` for relevant conventions if present. Read surrounding source and callers for evidence, but report only findings in the change.

| Input | Review this |
|---|---|
| `diff: patch <path>` | The supplied patch, including added files. |
| `diff: working` | `git diff HEAD` plus files from `git ls-files --others --exclude-standard`. |
| `diff: staged` | `git diff --cached`. |
| Branch review | Compare the supplied merge base with the specified head. Do not include working-tree edits. |

Check the relevant criteria and outline, if provided. Accept a justified outline deviation that preserves the contract. Flag unrequested scope and apply the shared reuse and readability checks to every diff.

Verify applicable test-first evidence and rerun necessary checks independently against the reviewed tree. For a patch or staged diff, exclude unrelated edits from the check; for a branch, check the specified head. If you cannot run a required check against that tree, report the gap and request changes. Under `## Signals verified`, report what ran. Report `comments: N added (M over max)` for the same diff; compare counts only when scopes match. Check any proposed commit message against `atomic-git-discipline`.

Report under `## Spec compliance`, `## Code quality`, and `## Signals verified`. For a branch without a spec, put `(no spec — branch review only)` under `## Spec compliance`.

## Spec draft


Check the supplied spec against its design and repo evidence. Look for missing behavior, rules, or interface decisions, contradictions, code-level prescriptions, and criteria that cannot be tested. Apply the reuse and YAGNI checks to the proposed scope: flag needless work, duplicate helpers or patterns, and departures from existing library usage. Cite the existing path or symbol that makes the extra work unnecessary.

Check the fenced change tree and outline against the template. Accept links to design flows and require numbered steps only for uncovered flows. Accept `None` with a reason when no flow applies. Check that each checkpoint is a cohesive slice with a named check, and that end-to-end proof names setup, action, and observable result separately. If proof cannot run here, require a limitation and owner. Check that risks have likelihood and mitigation, and that the body reflects current decisions.

Report under `## Design coverage` and `## Spec quality`. Omit signals.

## Wiki page


When asked to validate a wiki page, use the supplied page, source paths, and checklist. Verify claims and diagrams against source. Do not require a code diff, spec, or test signals. Report source-only issues under `## Concerns (do not include in domain file)` before totals and verdict. Exclude those concerns from the page's findings and verdict; report any resulting page inaccuracy or checklist failure as a separate finding.

## Verdict


Preserve the output headings even when empty. End with `totals:` and exactly one of `VERDICT: PASS` or `VERDICT: CHANGES_REQUESTED`.

Return `PASS` only when required checks pass and no findings remain. A scoped diff may pass with explicitly labeled advisory nits only if the caller sets `allow-advisory: true`. Never defer a risk, bug, or unresolved question. Report each nit with its confidence.
