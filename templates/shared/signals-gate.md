{{define "signals-gate"}}**Signals pre-commit** — evaluate these gates in order; stop at the first that fails:
    1. `command -v atomic` succeeds? If not, skip.
    2. `atomic signals stale` exits 1 (stale)? If it exits 0 (fresh), skip.

    Both pass → invoke the `atomic-signals` skill in silent mode (no report line). If signals regenerate, stage `.claude/project/deterministic-signals.md` and `.claude/project/signals.md`.

    No file-extension allowlist. `atomic signals stale` is the source of truth; it fast-fails when nothing changed and catches structural shifts (e.g. a new `commands/*.md` file) that an extension list would miss.{{- end}}
