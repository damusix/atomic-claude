{{define "doc-impact"}}**Documentation impact check** — invoke the `atomic-documentation` skill on the staged diff (`git diff --cached`). Parse the last fenced `yaml`/`yml` block per the parser contract in `skills/atomic-documentation/SKILL.md`. If the block is missing, unparseable, has no `surfaces` key, or `surfaces` is empty, skip this step silently. For each non-empty surface:
    - Print: `surface <N>/<total>: <path> (<voice>) — <reason>`
    - Prompt: `[e] edit  [s] skip with reason  [c] continue (misclassification)`
    - **edit**: open the file, apply the suggested change, stage it with `git add <path>`.
    - **skip**: ask for a typed reason; record `doc-skip: <reason>` to append to the commit trailer block (after the body's blank line, in `git interpret-trailers --parse` range). One line per skip.
    - **continue**: treat as misclassification; no edit, no `doc-skip` line.{{- end}}
