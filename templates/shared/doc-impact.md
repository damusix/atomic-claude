{{define "doc-impact"}}<doc-impact>
Check whether the staged changes affect documentation. Invoke the `atomic-documentation` skill on `git diff --cached`.

Parse the last fenced `yaml`/`yml` block per the parser contract in `skills/atomic-documentation/SKILL.md`. If the block is missing, unparseable, or has no surfaces, skip silently.

For each surface found:
- Print: `surface <N>/<total>: <path> (<voice>) — <reason>`
- Prompt: `[e] edit  [s] skip with reason  [c] continue (misclassification)`
- **edit** — open the file, apply the change, stage with `git add <path>`.
- **skip** — ask for a typed reason; record `doc-skip: <reason>` as a commit trailer.
- **continue** — treat as misclassification; move on.

Run doc-impact before signals refresh so that new doc files get picked up by signals in one pass.
</doc-impact>{{- end}}
