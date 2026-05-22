---
id: install-output-style-f-1
title: '`prompt.Confirm` default-value plumbing is untested'
created: "2026-05-21"
origin: |
    install-output-style branch (abandoned 2026-05-21), iter 1 reviewer. Logged here because the prompt package was salvaged forward.
severity: risk
review_by: "2026-07-20"
status: open
---

`atomic/internal/prompt/prompt_test.go:28-50` + `prompt.go:54`


Both Confirm tests stub the runner entirely; neither exercises the path where `def` reaches `defaultRunConfirm` and survives huh's form init. The `result = def` line could be deleted with no test failure. Hard to test cleanly without a real TTY. Revisit when adding any integration harness for huh, or when huh ships a `.Default(bool)` builder we can rely on directly.
