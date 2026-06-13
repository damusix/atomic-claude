# Changelog

## [5.0.0](https://github.com/damusix/atomic-claude/compare/v4.5.0...v5.0.0) (2026-06-13)


### ⚠ BREAKING CHANGES

* consolidate artifact surface 50→35

### Features

* **code-intel:** realm federation for atomic code ([8be144b](https://github.com/damusix/atomic-claude/commit/8be144bcbeb7adf096eba04e2a8d5effe9290073))


### Bug Fixes

* **atomic-plan:** spec body is forward-only, no design duplication ([7f87ca7](https://github.com/damusix/atomic-claude/commit/7f87ca73c27f9d44822a44a84c9f39802997869a))
* consolidate artifact surface 50→35 ([3252e57](https://github.com/damusix/atomic-claude/commit/3252e5772faffd034ad27050191e5153a09b6c1c))

## [4.5.0](https://github.com/damusix/atomic-claude/compare/v4.4.0...v4.5.0) (2026-06-12)


### Features

* **wiki:** atomic-wiki skill — conversational entry point for buckets and realms ([22f52c3](https://github.com/damusix/atomic-claude/commit/22f52c3efd0c3d5aa3fd320951ef8486fc8667d6))
* **wiki:** capture buckets — fingerprinted folders synthesized into wiki knowledge ([c640602](https://github.com/damusix/atomic-claude/commit/c640602499da014d6429ec3931c81489ebbf5c35))


### Bug Fixes

* **wiki:** discover summaries on disk so summarized status is reachable ([e3b0f31](https://github.com/damusix/atomic-claude/commit/e3b0f319940202d3dc9c0a5d6eb96e505b4ce4e4))

## [4.4.0](https://github.com/damusix/atomic-claude/compare/v4.3.0...v4.4.0) (2026-06-11)


### Features

* **commit:** cap commit/PR verbosity, drop PR test plan ([167d7f3](https://github.com/damusix/atomic-claude/commit/167d7f3c86f1632654af2ed9ad266d965bc88264))
* **install:** block-aware CLAUDE.md update and diff ([badfb90](https://github.com/damusix/atomic-claude/commit/badfb90baac0a0b3f40103d3b09bcb6275713d84))
* **update:** auto-refresh ~/.claude artifacts before doctor ([b98f8b7](https://github.com/damusix/atomic-claude/commit/b98f8b7dd7171432f18386162543e82bed0523fd))
* **update:** make artifact refresh default; drop managed-install gate ([d7bc667](https://github.com/damusix/atomic-claude/commit/d7bc667b94f008dba3d8d38d42403bc7a3905906))


### Bug Fixes

* **update:** strip leading v from user-facing update version strings ([64d4c0c](https://github.com/damusix/atomic-claude/commit/64d4c0c8427010d75e5dd17eceb03c948788aea8))

## [4.3.0](https://github.com/damusix/atomic-claude/compare/v4.2.0...v4.3.0) (2026-06-10)


### Features

* **validate:** lint CLI verb/flag citations in artifacts ([09e9c27](https://github.com/damusix/atomic-claude/commit/09e9c275e04813e2418b7a1132f6336c6b8f6220))
* **verify:** add atomic validate to ship gate ([5f2c930](https://github.com/damusix/atomic-claude/commit/5f2c930edc158393cc85b7edd0d6bfbfd63514c9))

## [4.2.0](https://github.com/damusix/atomic-claude/compare/v4.1.0...v4.2.0) (2026-06-10)


### Features

* **codeintel:** extract embedded SQL across 20 host languages ([#44](https://github.com/damusix/atomic-claude/issues/44)) ([92935b0](https://github.com/damusix/atomic-claude/commit/92935b036530e1f482d19e913941f2d1a6b8e4d7))

## [4.1.0](https://github.com/damusix/atomic-claude/compare/v4.0.0...v4.1.0) (2026-06-08)


### Features

* code-intelligence engine + workflow integration ([045e5ce](https://github.com/damusix/atomic-claude/commit/045e5ce997e092b8e08fc2ce8f927d406c2da239))


### Bug Fixes

* surface atomic code explore across the workflow; correct CLI-coordination drift ([0d24f22](https://github.com/damusix/atomic-claude/commit/0d24f22ba8dc9afdbf7331879947e87f0e55c1aa))

## [4.0.0](https://github.com/damusix/atomic-claude/compare/v3.3.0...v4.0.0) (2026-06-07)


### ⚠ BREAKING CHANGES

* the output.intensity config key is removed. An existing config.toml entry for it now parses as an unknown key (warned and ignored) rather than erroring; no crash, but the key no longer does anything.

### Features

* output-style clarity overhaul; remove output.intensity ([b8fb5ec](https://github.com/damusix/atomic-claude/commit/b8fb5ec0861fbb7c5274acebcc5057228eb74e2b))

## [3.3.0](https://github.com/damusix/atomic-claude/compare/v3.2.0...v3.3.0) (2026-06-07)


### Features

* navigable markdown links in signals and wiki artifacts ([34613f2](https://github.com/damusix/atomic-claude/commit/34613f296e043f5977792961b8e4e5db758d1f63))

## [3.2.0](https://github.com/damusix/atomic-claude/compare/v3.1.1...v3.2.0) (2026-06-06)


### Features

* **wiki:** cross-repository project wikis ([049818b](https://github.com/damusix/atomic-claude/commit/049818bd551ba0f2e29274771d728b1f6653783e))

## [3.1.1](https://github.com/damusix/atomic-claude/compare/v3.1.0...v3.1.1) (2026-06-06)


### Bug Fixes

* skip bundle/manifest checks outside the atomic-claude repo ([af9a323](https://github.com/damusix/atomic-claude/commit/af9a3238a4a92d96f86ec1c0077dc8d34c34d70a))
* skip bundle/manifest checks outside the atomic-claude repo ([3204206](https://github.com/damusix/atomic-claude/commit/3204206d5184b752a4c4a4efbc72d20eebaa0f96)), closes [#35](https://github.com/damusix/atomic-claude/issues/35)

## [3.1.0](https://github.com/damusix/atomic-claude/compare/v3.0.1...v3.1.0) (2026-06-06)


### Features

* **agents:** prefer ast-grep for code search ([ad65d24](https://github.com/damusix/atomic-claude/commit/ad65d24101c4310d9f8edaf79c911759271c8c47))


### Bug Fixes

* **cli:** content-based stale + unified exit codes ([ea22b8c](https://github.com/damusix/atomic-claude/commit/ea22b8ccfe5d4c1119f3a0733a961d159e85cf5f))

## [3.0.1](https://github.com/damusix/atomic-claude/compare/v3.0.0...v3.0.1) (2026-05-31)


### Bug Fixes

* **docs:** detect deleted docs in stale check ([76950a0](https://github.com/damusix/atomic-claude/commit/76950a04140eb24e4deb1ff9edb72b3d2099dd35))
* **doctor:** realize --verbose, add remediation lines, double-dash help ([19c2f8a](https://github.com/damusix/atomic-claude/commit/19c2f8a9f67a3242304cb1c07d35f7709edb19d1))
* **validate:** scope C5 @-ref check to CLAUDE.md only ([2f108ab](https://github.com/damusix/atomic-claude/commit/2f108ab041c27214cd65b1a4e2ddd717c0de820d))

## [3.0.0](https://github.com/damusix/atomic-claude/compare/v2.1.0...v3.0.0) (2026-05-30)


### ⚠ BREAKING CHANGES

* `atomic hooks install` no longer creates .claude/hooks/session-start-reminders.sh. Existing installs migrate to the inline command on the next install/uninstall.

### Features

* **autopilot:** hands-off end-to-end delivery command ([7c6f836](https://github.com/damusix/atomic-claude/commit/7c6f8368e06c9141d3bbac6f7e2793206ee65452))
* **followups:** typed follow-up ledger (finding|plan) ([0ef7e15](https://github.com/damusix/atomic-claude/commit/0ef7e1537541fc4d11464ab3fa756c1b4f7629f5))
* **subagent-loop:** stuck-fix escalation + suppression awareness ([839895b](https://github.com/damusix/atomic-claude/commit/839895bec98297a19e87ee3437d1cae4d8449cec))


### Bug Fixes

* inline session-start hook, fix doctor scope ([3a48ede](https://github.com/damusix/atomic-claude/commit/3a48ede901a54f293d1748b2bec77a5e4f5992ad))
* **planning:** specs keep the body current, name the phase-gates ([5427e28](https://github.com/damusix/atomic-claude/commit/5427e281db998fa4cdcda400eff8b69acb4c8a2f))

## [2.1.0](https://github.com/damusix/atomic-claude/compare/v2.0.0...v2.1.0) (2026-05-29)


### Features

* **profile:** add no-hooks refresh fallback to CLAUDE.md preamble ([3e7af16](https://github.com/damusix/atomic-claude/commit/3e7af16ccde4bb2c7ba2824971398793fa277ed1))
* **profile:** bound per-tool version detection with a ~3s timeout ([b3ce841](https://github.com/damusix/atomic-claude/commit/b3ce8417a47d1ae0780da51357a514413f58c715))
* **profile:** name version managers, real elixir/mix versions, omz scripts ([61f8736](https://github.com/damusix/atomic-claude/commit/61f87369a0eebdfe86395747669a61a91c0c6055))
* **profile:** populate the env fingerprint at install/update ([4370114](https://github.com/damusix/atomic-claude/commit/437011480af1eb1248082f02355248be3da3fed6))
* **profile:** refresh dev-tooling env block in profile.md ([2e359ca](https://github.com/damusix/atomic-claude/commit/2e359ca5511b2e0d4a7fa3ec724fe3d608311242))


### Bug Fixes

* trim CLAUDE.md catalog bloat + internal refs ([bac3b33](https://github.com/damusix/atomic-claude/commit/bac3b3302c68a43a0a788ad3b803e7b09beab991))

## [2.0.0](https://github.com/damusix/atomic-claude/compare/v1.9.1...v2.0.0) (2026-05-28)


### ⚠ BREAKING CHANGES

* consolidate signals skill into agent; remove /atomic-compress; collapse voices; rewrite axiom 2

### Features

* add /atomic-improve, /gather-evidence; overhaul /atomic-help ([b21803d](https://github.com/damusix/atomic-claude/commit/b21803dc14361dae29ed8297283f2eec638cfffa))
* **profile:** implement global user-profile feature ([7945184](https://github.com/damusix/atomic-claude/commit/79451848c8c89d3cd1d6e7befd1624f5a6c70ebb))


### Bug Fixes

* consolidate signals skill into agent; remove /atomic-compress; collapse voices; rewrite axiom 2 ([0f3ca7d](https://github.com/damusix/atomic-claude/commit/0f3ca7dc723d916ce35a2a2fa2f273e00b6fbca1))
* **subagent-implementation:** auto-invoke /worktree-start on Yes ([b55e1e8](https://github.com/damusix/atomic-claude/commit/b55e1e8c5d4062e218aa112eab108c564dbf8a29))

## [1.9.1](https://github.com/damusix/atomic-claude/compare/v1.9.0...v1.9.1) (2026-05-26)


### Bug Fixes

* **version:** clarify default sentinel values in package doc ([27b323d](https://github.com/damusix/atomic-claude/commit/27b323d82f24b513a448db3f3c352bffb5d50db1))

## [1.9.0](https://github.com/damusix/atomic-claude/compare/v1.8.0...v1.9.0) (2026-05-26)


### Features

* **signals:** steering file bootstrap and inferrer pass-through ([9c94772](https://github.com/damusix/atomic-claude/commit/9c94772f5e2ccf93eca0c73676ff279eed341b8b))
* XML structural tags, &lt;atomic&gt; ownership boundary, single @-ref ([091d4ef](https://github.com/damusix/atomic-claude/commit/091d4ef0002eebb7d443775acbd6b92539859ff6))

## [1.8.0](https://github.com/damusix/atomic-claude/compare/v1.7.0...v1.8.0) (2026-05-25)


### Features

* documentation-as-maintenance system ([f3d1230](https://github.com/damusix/atomic-claude/commit/f3d1230ee37dd915cfc64be9faf603d20db42895))
* VitePress docs site, documentation rewrite, prompting best practices ([f9ff26c](https://github.com/damusix/atomic-claude/commit/f9ff26ca4b07e749107c7317153e47bf80be0011))


### Bug Fixes

* **prose:** add "ship" to marketing jargon blocklist ([ef46546](https://github.com/damusix/atomic-claude/commit/ef46546d1e3c86ba712383426aa27a4bee5b562e))

## [1.7.0](https://github.com/damusix/atomic-claude/compare/v1.6.0...v1.7.0) (2026-05-25)


### Features

* **signals:** steering file, dual-mode .signalsignore, docs ([c02187a](https://github.com/damusix/atomic-claude/commit/c02187a0453202b43cc7e391d42152964085c16b))

## [1.6.0](https://github.com/damusix/atomic-claude/compare/v1.5.1...v1.6.0) (2026-05-24)


### Features

* **install:** pre-install snapshot + `atomic claude uninstall` ([2c2c166](https://github.com/damusix/atomic-claude/commit/2c2c16621cfed852d4590b616a168aea2fbc3b9d))

## [1.5.1](https://github.com/damusix/atomic-claude/compare/v1.5.0...v1.5.1) (2026-05-23)


### Bug Fixes

* **signals:** replace stale inferred-signals.md refs with signals.md ([27eca57](https://github.com/damusix/atomic-claude/commit/27eca57520e2c0df68dc7692772518978e5c75f2))

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
