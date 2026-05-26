{{define "staleness-check"}}
<staleness-check>

Before continuing, check whether signals or documentation may be out of date. This is advisory — ask the user and accept their answer. **Why:** the next session benefits from a fresh project snapshot; stale signals cause hallucinated file references.

1. **Signals** — run `command -v atomic && atomic signals stale`. If stale (exit 1), ask: "Signals are stale — refresh before continuing?" Accept yes or no.
2. **Documentation** — run `git diff <base>..HEAD --name-only` to get changed files. Invoke `atomic-documentation` in dry-run mode. If it identifies surfaces that may need updating, summarize them and ask: "These docs may be outdated: <list>. Update before continuing?" Accept yes or no.

If the user declines, proceed without further prompting.

</staleness-check>{{- end}}
