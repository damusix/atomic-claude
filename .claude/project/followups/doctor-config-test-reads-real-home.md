---
id: doctor-config-test-reads-real-home
title: TestRepairPlan_configWARN_fixable reads the real ~/.atomic config
created: "2026-07-16"
origin: |
    harness-autodetect quick-fix verify, 2026-07-16
kind: finding
severity: risk
review_by: "2026-09-14"
status: open
file: atomic/internal/doctor/checks_config_test.go
---

The test drives checkConfig/applyConfigRepair through production home resolution (os.UserHomeDir), so it reads the real ~/.atomic/config.toml. On any migrated dev machine whose config carries install.version="dev" (every dev-build install), the repair path hits the semver validation error and the test fails persistently. Masked pre-v6 only because ~/.atomic did not exist. Fix: inject home (temp dir) as a seam, same pattern as RunCheckConfigWith's test siblings.
