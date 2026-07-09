# install.sh deprecation decision

Date: 2026-07-09
Issue: [#127](https://github.com/damusix/atomic-claude/issues/127) — "Deprecate install.sh + stale manual-copy dogfooding language"


## Problem

A todo proposed deprecating `install.sh` and cleaning up stale manual-copy/dogfooding install
language elsewhere in the repo. Issue #127 framed two options:

1. Full deprecation — remove `install.sh`, replace with an alternative bootstrap.
2. Language-only cleanup — keep `install.sh`, fix stale prose that describes an older,
   pre-binary install flow.


## Decision

Keep `install.sh` as the supported binary bootstrap. Do language-only cleanup (option 2).

No file deletions, no install-flow changes.


## Evidence

**Functional dependency — emitted Dockerfiles fetch it at build time.**
`atomic/internal/dockerinit/templates/Dockerfile.tmpl:28-29`:

    ENV ATOMIC_VERSION={{.AtomicVersion}}
    RUN curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash

Every eval Dockerfile previously emitted by `atomic docker init` pulls
`install.sh` from the `main` branch raw URL at container build time, pinned to a version via
`ENV ATOMIC_VERSION`. Deleting the script from `main` breaks every previously emitted
Dockerfile retroactively, not just future ones — there is no way to un-ship that dependency
without also managing a compatibility window across arbitrary already-built container images.

**Tested.** `atomic/test/install_sh_test.go` runs `bash -n` (syntax check), `shellcheck`, and
function-level unit tests (`TestSemverGte_BasicOrdering`, `TestOsDetection_WindowsVariants`,
extracting and exercising individual shell functions like `_os()` and `_semver_gte()` in
isolation). The script carries real test coverage and is not an unmaintained relic.

**No replacement bootstrap exists.** `.goreleaser.yaml` configures no Homebrew tap (`brews:`
section absent) — there is no `brew install atomic-claude` one-liner to fall back to. The
documented alternatives in `docs/guides/install.md` are secondary paths, not one-liner
replacements:

- Manual install (`docs/guides/install.md:129-131`) — download an archive from GitHub Releases,
  verify the checksum, move the binary onto `$PATH` by hand.
- Build from source (`docs/guides/install.md:134-140`) — `git clone` + `make build`.

Removing `install.sh` without a replacement one-liner would regress every user who currently
runs the single `curl … | bash` command to a multi-step manual process.

**Operational references across the artifact surface.** Three command templates embed the
`curl … install.sh | bash` string as an operational instruction, not incidental prose:

- `templates/commands/setup-wiki.md:98,301` (renamed from `atomic-setup` in #139) — printed as
  the remediation command when the `atomic` binary is missing.
- `templates/commands/refresh-wiki.md:64,188` — cited in an `install:` field.
- `templates/commands/report-issue-with-atomic.md:61` — listed as one of the install-method
  options an issue reporter selects from.

Each of these templates has a rendered copy (`commands/*.md`) and an embedded bundle copy
(`atomic/internal/embedded/bundle/commands/*.md`). Removing `install.sh` would require editing
all three template sources, re-running `make render` and `make -C atomic bundle` to propagate
the change through both generated layers, and rewriting three operational flows — this is a
multi-surface breaking change, not a docs-only cleanup.


## Path not taken: full deprecation

Rejected. All four evidence points above compound against it: it breaks a live external
dependency (emitted Dockerfiles) retroactively, discards working test coverage, has no
one-liner replacement to fall back to (no brew tap), and requires a coordinated render+bundle
regen across three command flows. Nothing in the issue's framing justified accepting that cost.


## What was actually stale

Only `claude.local.md`'s `## Install` section:

    No install script yet. Manual: copy each top-level directory into `~/.claude/`, restart
    Claude Code. A future `/install` or Makefile target is on the table.

This predates `atomic claude install` and describes an install flow the repo no longer has.
The file is tracked at the repo root; the section is rewritten in this change to point at the
supported path (`install.sh` → `atomic claude install`, dev binary via `make -C atomic build`).

Tracked docs were swept and are already current. `README.md` and `docs/guides/install.md` both
document the single supported path: `curl -fsSL https://raw.githubusercontent.com/damusix/atomic-claude/main/install.sh | bash`
followed by `atomic claude install`. No edits were needed there.
