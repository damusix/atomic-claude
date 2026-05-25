---
description: Squash all commits on current branch into one. No merge. Synthesized commit message via atomic-commit skill. Does not touch base.
---

{{ template "squash-flow" . }}

## Report

`squashed N commits into <new-sha>. branch still <branch>.`

{{ template "git-safety" . }}
