# Changelog

## [1.5.0](https://github.com/damusix/atomic-claude/compare/v1.4.1...v1.5.0) (2026-05-23)


### Features

* **signals:** vertical-slice domain partitioning ([3a9c645](https://github.com/damusix/atomic-claude/commit/3a9c6454035d88293a652b55e744f41cbe1c0777))

## [1.4.1](https://github.com/damusix/atomic-claude/compare/v1.4.0...v1.4.1) (2026-05-23)


### Bug Fixes

* **docs,bundle:** sync docs with 1.2-1.4 reality + ship _templates ([b2f6514](https://github.com/damusix/atomic-claude/commit/b2f65144d2fb5e00a0c4a76790ec59fabfd248c9))

## [1.4.0](https://github.com/damusix/atomic-claude/compare/v1.3.0...v1.4.0) (2026-05-23)


### Features

* **pressure-test:** verify codebase claims before asserting ([ac096c8](https://github.com/damusix/atomic-claude/commit/ac096c810be2c0f76de9d0ee4da7e1ef4acaa1ff))
* **render:** add render-templates tool + bootstrap ([8347bbe](https://github.com/damusix/atomic-claude/commit/8347bbe1978d173b72badfdd62d99630134e7a51))
* **setup:** survey-driven CLAUDE.md scaffold ([cda3053](https://github.com/damusix/atomic-claude/commit/cda3053b0bea66b2d80f8f79e25a5f11d952b22a))
* **signals:** expand language detection to 26 languages ([6e5a0b7](https://github.com/damusix/atomic-claude/commit/6e5a0b7eaabf51a37b663ded25792396d012f135))
* **signals:** router-shaped signals.md + content-SHA change detection ([f15c325](https://github.com/damusix/atomic-claude/commit/f15c3256377f856a5814d6ff0ff01cc1489c2dd5))

## [1.3.0](https://github.com/damusix/atomic-claude/compare/v1.2.1...v1.3.0) (2026-05-23)


### Features

* **atomic-plan:** redesign for triviality tiers + spec loop ([cd43182](https://github.com/damusix/atomic-claude/commit/cd43182e853971864e522a899af0396d60e2b145))
* **config:** atomic config CLI + ~/.claude/.atomic/ state directory ([5c9d61c](https://github.com/damusix/atomic-claude/commit/5c9d61cc1ab3e82d260352261767002fdb8d4d7a))
* **documentation:** split skill from command, wire ship verbs ([4d7a7bf](https://github.com/damusix/atomic-claude/commit/4d7a7bf45e46699efb77553b32400b9aa0c4ddcb))
* **followups:** per-entry folder + INDEX + CLI ([1ff0a3b](https://github.com/damusix/atomic-claude/commit/1ff0a3bb7037c6b9a31fa03918221392d73c8e8a))
* **prompt:** add internal/prompt + axiom softenings + contrib skill ([2a06664](https://github.com/damusix/atomic-claude/commit/2a06664c70dc852086fdd9a42640339d148dc7a6))
* **update:** auto-run doctor after self-update ([bf54309](https://github.com/damusix/atomic-claude/commit/bf543099ed86fce523740c483e10f786227242c0))
* **worktree-start:** carry forward in-context specs ([d61ff4a](https://github.com/damusix/atomic-claude/commit/d61ff4a27d1d50b09776c456f8f86ecf1747da75))

## [1.2.1](https://github.com/damusix/atomic-claude/compare/v1.2.0...v1.2.1) (2026-05-18)


### Bug Fixes

* **commands:** add /atomic-help routing assistant ([633c7c7](https://github.com/damusix/atomic-claude/commit/633c7c7483671f3968be1717121a012e647aca8b))

## [1.2.0](https://github.com/damusix/atomic-claude/compare/v1.1.0...v1.2.0) (2026-05-18)


### Features

* **agents:** add atomic-strategist (opus) ([#3](https://github.com/damusix/atomic-claude/issues/3)) ([32db48c](https://github.com/damusix/atomic-claude/commit/32db48cb5dfb172b51bdf035bed276b50fb45f42))
* **commands:** /subagent-diagnose orchestrator (ci + bug modes) ([f1c4f3a](https://github.com/damusix/atomic-claude/commit/f1c4f3a2d1034e5525552f8cdab97630536119da))
* **commands:** add /pressure-test challenger ([#9](https://github.com/damusix/atomic-claude/issues/9)) ([3942a65](https://github.com/damusix/atomic-claude/commit/3942a65006b7c22af6d1de85da824da76f77e9e8))
* **doctor:** atomic doctor health-check subcommand ([#7](https://github.com/damusix/atomic-claude/issues/7)) ([26e3ab1](https://github.com/damusix/atomic-claude/commit/26e3ab1347fd88dd6d3fcde329fe931c86b2dd17))
* **reminder:** hybrid cron/Routines transport with past-due hook surfacing ([#4](https://github.com/damusix/atomic-claude/issues/4)) ([1616ad5](https://github.com/damusix/atomic-claude/commit/1616ad5428758793d3051e143c11279f49579bc8))
* **validate:** add atomic validate artifact linter (v1) ([82cd9cc](https://github.com/damusix/atomic-claude/commit/82cd9cc22fac5cd79b729459356771810c6a3c0d))

## [1.1.0](https://github.com/damusix/atomic-claude/compare/v1.0.0...v1.1.0) (2026-05-17)


### Features

* **commands:** add /commit-and-push and /push-only ([f971266](https://github.com/damusix/atomic-claude/commit/f971266893706aeaa71ed56f1927d6c371b01622))
* **commands:** add /refresh-signals + sync stale bundle ([8e4d40e](https://github.com/damusix/atomic-claude/commit/8e4d40e48f26d1c9a459e52e2123029986621750))
* **commands:** add /watch-ci for non-blocking CI observation ([42dbb48](https://github.com/damusix/atomic-claude/commit/42dbb485fe7ec22edea4b7d93a0713bc9ccee92e))
* **docker:** add dual-mode eval environment ([2a0dd89](https://github.com/damusix/atomic-claude/commit/2a0dd890d2a19e79e74690989ad2801b133271b1))
* **install-workflow:** /atomic-claude-merge command and atomic-claude-merger agent ([e6cf258](https://github.com/damusix/atomic-claude/commit/e6cf25856a4d62df01f2865d8959c96e0eda5013))
* **session-report:** add /session-report verb ([fc3e7fb](https://github.com/damusix/atomic-claude/commit/fc3e7fb7ba3697f5db6e0895f05711951cd02d4e))
* **signals,bundle:** thorough full-mode inference + permissive bundling ([49ca01f](https://github.com/damusix/atomic-claude/commit/49ca01f11b0e90327567bcecc0ac6b9add682719))


### Bug Fixes

* **commit-only:** drop source-extension allowlist from signals gate ([17c699c](https://github.com/damusix/atomic-claude/commit/17c699c41b13dd658635bc9716701b68b93b12a5))

## 1.0.0 (2026-05-17)


### Features

* **atomic:** CP-1 module skeleton ([e029254](https://github.com/damusix/atomic-claude/commit/e02925439ff8ff5ef2e478ebcb8f3d354a0f83cc))
* **atomic:** CP-2 signals subcommand ([0cb1362](https://github.com/damusix/atomic-claude/commit/0cb1362ab468a5facd7d5c6ff2df7731567c069c))
* **atomic:** CP-3 reminder subcommand ([7964274](https://github.com/damusix/atomic-claude/commit/7964274613a87da1af6bb78f2d7e8694fba78fb6))
* **atomic:** CP-4 hooks subcommand ([6117f9a](https://github.com/damusix/atomic-claude/commit/6117f9a035613c73893cac544497ddf38614ed05))
* **atomic:** CP-5 claude bundle subcommand ([93c7531](https://github.com/damusix/atomic-claude/commit/93c753186a5b7f07806f04acbede00feb579c9d9))
* **atomic:** CP-6 self-update + background check ([57f9d26](https://github.com/damusix/atomic-claude/commit/57f9d264e04321b25e06e2427ddc730e0d274a81))
* CP-7 release pipeline + installer ([fb4b20d](https://github.com/damusix/atomic-claude/commit/fb4b20deaf10092a1a13b81ea0b87eafd3b32d67))
* **signals:** annotate tree directories with child counts ([6c6941f](https://github.com/damusix/atomic-claude/commit/6c6941ff84018602dd4180fe8922bec15e6dfb52))


### Bug Fixes

* **bundlemirror:** source rules from repo root, not .claude/ ([e7eebeb](https://github.com/damusix/atomic-claude/commit/e7eebeb64bb1b8773e0fa1964894a96058fa2ea3))
* drain FOLLOWUPS — polish across all packages ([72cbcde](https://github.com/damusix/atomic-claude/commit/72cbcde2f01b959ddc854bf89de446b1136d90b6))
* iter-11 review fixups ([f5080d2](https://github.com/damusix/atomic-claude/commit/f5080d226604b0db845a66d0b002d245b637a317))
* **signals:** plural agreement + render dir entries at depth cap ([ee54fc0](https://github.com/damusix/atomic-claude/commit/ee54fc0883e586a380568fe3a7fe7bb00eee2ab7))
* **signals:** real tree shape, recursive manifests, ordered frontmatter ([eb2f979](https://github.com/damusix/atomic-claude/commit/eb2f979843622e039eab385298fcbea1b319cbb2))
