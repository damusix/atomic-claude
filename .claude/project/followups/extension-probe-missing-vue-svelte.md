---
id: extension-probe-missing-vue-svelte
title: extensionCandidates knownExts lacks .vue/.svelte — relative component imports unresolved
created: "2026-08-08"
origin: |
    code-intel plan implementation validation 2026-08-08
kind: finding
severity: nit
review_by: "2026-10-07"
status: open
file: atomic/internal/codeintel/resolution/resolver.go:211
---

import './SessionPlayer.vue' probes SessionPlayer.vue.ts etc. because .vue is not a recognized extension; the real file: node exists but is never probed. Add .vue/.svelte to knownExts.
