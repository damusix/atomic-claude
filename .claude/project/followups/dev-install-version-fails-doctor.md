---
id: dev-install-version-fails-doctor
title: Dev-build install writes install.version='dev', which fails doctor config check
created: "2026-07-22"
origin: |
    agents-effort-config local install
kind: finding
severity: nit
review_by: "2026-09-20"
status: open
file: atomic/internal/config/config.go
---

After 'make -C atomic build' + 'bin/atomic claude install', ~/.atomic/config.toml gets [install] version = 'dev' (the un-ldflagged dev version). config.Validate requires a parseable semver, so 'atomic doctor' immediately reports FAIL: install.version "dev" is not a valid semver string. Every contributor testing a dev build hits a red doctor. This is also the underlying cause of the flaky doctor-config-test-reads-real-home test, which reads the real ~/.atomic and fails when install.version is 'dev'. Options: accept 'dev' as a valid sentinel that skips migrations, or skip writing install.version for non-release builds.
