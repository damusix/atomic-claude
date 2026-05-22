---
id: install-output-style-f-2
title: huh PointerAccessor read-timing risk on huh upgrade
created: "2026-05-21"
origin: |
    install-output-style branch (abandoned 2026-05-21), iter 1 reviewer. Logged here because the prompt package was salvaged forward.
severity: risk
review_by: "2026-07-20"
status: open
file: atomic/internal/prompt/prompt.go:44-58
---

`huh.NewConfirm()` has no explicit `.Default(bool)` builder. Current code sets `result = def` after `Value(&result)` and before `form.Run()`, relying on huh's `PointerAccessor` reading through the pointer at render time. If a future huh version resets bound values during form init, every confirm silently becomes `false`. huh's minor version is pinned in `go.mod`; revisit on every huh upgrade.
