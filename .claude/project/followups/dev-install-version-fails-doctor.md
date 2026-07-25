---
id: dev-install-version-fails-doctor
title: Dev-build install writes install.version='dev', which fails doctor config check
created: "2026-07-22"
origin: |
    agents-effort-config local install
kind: finding
severity: risk
review_by: "2026-09-20"
status: open
file: atomic/internal/config/config.go
---

After `make -C atomic build` + `bin/atomic claude install`, `~/.atomic/config.toml` gets
`[install] version = 'dev'` (the un-ldflagged dev version). `config.Validate` requires a
parseable semver, so `atomic doctor` immediately reports
`FAIL: install.version "dev" is not a valid semver string`. Every contributor testing a dev
build hits a red doctor.

**It also breaks the test suite for every contributor.**
`TestRepairPlan_configWARN_fixable` (`internal/doctor/checks_config_test.go`) drives
`checkConfig`/`applyConfigRepair` through production home resolution (`os.UserHomeDir`), so it
reads the real `~/.atomic/config.toml` and hits the same semver failure. On any dev machine
that has ever run a dev-build install, `go test ./...` is permanently red in
`internal/doctor` — masked pre-v6 only because `~/.atomic` did not exist.

Observed cost, 2026-07-25: this was the standing baseline failure across an entire autopilot
session. Every verification step had to carve out "except the pre-existing doctor failure",
which is exactly the habit that lets a real regression through unnoticed.

**Options:** accept `dev` as a valid sentinel that skips migrations, or skip writing
`install.version` for non-release builds. Either way, also inject home as a seam in the test
(the pattern `RunCheckConfigWith`'s siblings already use) so it stops reading the real
`~/.atomic`.

Subsumes the closed entry `doctor-config-test-reads-real-home` (2026-07-25), which described
the test-side symptom of this same root cause.
