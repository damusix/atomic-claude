---
id: install-output-style-f-5
title: '`defaultRunConfirm` huh-abort mapping is test-dead'
created: "2026-05-21"
origin: |
    install-output-style branch (abandoned 2026-05-21), polish-pass reviewer. Logged here because the abort-handling code was salvaged forward.
severity: risk
review_by: "2026-07-20"
status: open
---

`atomic/internal/prompt/prompt.go:61-63` + `atomic/internal/doctor/stdin_prompter.go:42-44`


The `if errors.Is(err, huh.ErrUserAborted) { return false, ErrAborted }` line and the doctor-side `prompt.ErrAborted → DecisionAbort` translation cannot be reached without a real TTY. Tests cover the layers above (stubbed `runConfirm`, `Repair` loop respecting `DecisionAbort`) but not the actual mapping. Deleting either line would compile and pass `go test`. Structural gap — add an integration harness or a `huh`-side stub once the testability story for `huh` improves.
