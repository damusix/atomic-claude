# Changelog

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
