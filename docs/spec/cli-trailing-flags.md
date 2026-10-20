# CLI trailing flags: honored or rejected, never dropped

## Goal

On every verb under Scope, a flag placed after a positional argument is either honored or rejected with a usage error (exit 2; exit 1 for `reminder add`, its existing usage-error code), and an argument the verb does not take exits 2. Neither is silently ignored.

Every Cobra leaf in `atomic` sets `DisableFlagParsing` and hands raw argv to a stdlib `flag.FlagSet`. `FlagSet.Parse` stops at the first non-flag token, so in `atomic followups close <id> --reason "x"` the `--reason "x"` pair lands in `fs.Args()`. The handler reads only `fs.Args()[0]`, exits 0, and `CLOSED.md` gets a marker-only line. The same shape affects every verb listed under Scope.

## Non-goals

- No new verbs, no new flags, no flag renames.
- No change to exit codes other than: turning a silent drop into exit 2 (usage); honoring `-h` on `validate <sub>` (prints usage, exits 0); and a dash-leading word that used to be accepted as query, symbol, or reminder text now erroring as an unknown flag (exit 2 for `code`, exit 1 for `reminder add`, its existing usage-error code).
- Verbs that take no positional and only flags (`where`, `update`, `doctor`, `hooks *`, `claude *`, `signals scan|linkify`, `code index|sync|status|mcp|comments`, `wiki scan|stale|linkify|init`, `config list`, `repo init`, `docker init`, `profile refresh`, `retro extract`, `scratchpad list`) stay as they are. Outside `followups`, they are not in this change.
- Verbs that take positionals and no flags (`config get|set|unset`, `prompt`, `template`, `reminder show|rm|set-due`, `scratchpad path|archive`) stay as they are: they read their positionals and ignore anything after them.
- `atomic bus`, `atomic repl`, `atomic wiki bucket`, `atomic scratchpad new`, and `atomic docs index` already honor or reject trailing flags; their parsers are untouched.
- The global flags `--repo`, `--no-update-check`, and `-v`/`--version` are read from any position in argv before the verb runs, including after `--`, except on `migrate` and `config resolve`, where `--repo` is the verb's own flag. So are `--only` and `--exclude` on `code` verbs run at a realm root.
- No change to the `cliusage` golden or the A1 linter.

## Approach

One helper, `cliutil.ParseInterspersed(fs, args) ([]string, error)`. It splits argv at the first bare `--`, then calls `fs.Parse` repeatedly, setting aside one positional token at a time, so flags may sit before, after, or between positionals. Everything after `--` is positional, verbatim. Each verb keeps the stdlib grammar it already had: single-dash flags, `-h`/`--help` returning `flag.ErrHelp`, a flag's value token taken verbatim even when it starts with `-`. A verb that accepts a fixed number of positionals rejects any extra with exit 2 and a message naming the extra arguments.

## Scope

| Verb | Positionals | After this change |
|------|-------------|-------------------|
| `followups close` | exactly 1 `<id>` | `--reason` honored in any position; a 2nd positional → exit 2 |
| `followups list` | none | `--stale`/`--json` honored in any position; any positional → exit 2 |
| `followups add` | none | flags honored in any position; any positional → exit 2 |
| `followups render`, `followups path` | none | any argument → exit 2 |
| `serve` | 0–1 `[dir]` | `--port`/`--host`/`--open` honored after `dir`; a 2nd positional → exit 2 |
| `code search`, `code explore` | query words | flags after the query honored; positionals still join into the query |
| `code callers`, `code callees`, `code impact`, `code node` | symbol words | flags after the symbol honored; positionals still join into the symbol |
| `code files` | pattern words | flags after the pattern honored |
| `code affected` | file paths | flags after the paths honored; a flag token is never read as a path |
| `validate` (+ `spec`, `artifacts`, path dispatch) | subcommand and paths | `--json`/`--suggest` honored in any position; a flag token is never read as a path |
| `validate config`, `validate bundle` | none after the subcommand | flags honored in any position; any path → exit 2 |
| `migrate` | 0–1, only as `--show-log <since>` | flags honored after `<since>`; any other positional → exit 2 |
| `reminder add` | text words | `--due`/`--transport` honored after the text; positionals still join into the text |

## Success criteria

1. `atomic followups close <id> --reason "r"` writes `r` into `CLOSED.md`; so does `close --reason "r" <id>`.
2. `atomic followups close <id> extra` exits 2 and leaves `<id>.md` in place.
3. `atomic followups list x --json` and `atomic followups add stray --id ...` exit 2 without writing anything; `followups render x` and `followups path x` exit 2.
4. `atomic serve <dir> --port N` listens on `N`; `serve a b` exits 2.
5. Each `code` positional verb honors `--json` placed after its positionals (JSON output, positional text excludes the flag).
6. `atomic validate <path> --json` and `atomic validate spec <path> --json` emit JSON and do not treat `--json` as a path.
7. `atomic migrate extra` exits 2; `atomic migrate --show-log <since>` still works.
8. A bare `--` ends the verb's own flag scanning for every verb in scope: a verb flag after it is a positional.
9. Tests, one per affected verb, state why: a trailing flag is honored or rejected, never dropped. `go test ./...`, `go vet ./...`, and `gofmt -l .` are clean.
10. `docs/spec/follow-ups-folder.md` and `docs/spec/cli-cobra.md` describe the argument-order rule.
11. `atomic reminder add "<text>" --due <ts>` stores the due timestamp and the text without the flag; `validate config x` and `validate bundle x` exit 2.

## Checkpoints

| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Helper + every `followups` verb | `cliutil/args.go`, `followups/cli.go`, tests | implementer | 2 + tests | SC1, SC2, SC3, SC8 for followups |
| 2 | `serve`, `code` positional verbs, `validate`, `migrate` | `serve/serve.go`, `codeintel/cli/code.go`, `validate/*.go`, `cmd_migrate.go`, tests | implementer | 5–7 + tests | SC4–SC8 |
| 3 | Spec and doc amendments | `docs/spec/follow-ups-folder.md`, `docs/spec/cli-cobra.md`, `docs/reference/code-intel.md` | implementer | 3 | SC10 |
| 4 | `reminder add`; `validate config`/`validate bundle` reject paths | `cmd_reminder.go`, `validate/validate.go`, `docs/spec/atomic-validate.md`, `docs/spec/verify-gate-validate.md`, tests | implementer | 4 + tests | SC11 |

## Change tree

```
atomic/internal/cliutil/
  A args.go                ParseInterspersed, UnexpectedArgs
  A args_test.go
atomic/internal/followups/
  M cli.go                 every verb parses via ParseInterspersed; positional counts enforced
  M cli_test.go
atomic/internal/serve/
  M serve.go               parseFlags via ParseInterspersed; at most one dir
  M parseflags_internal_test.go
atomic/internal/codeintel/cli/
  M code.go                search/callers/callees/impact/node/files/explore/affected via ParseInterspersed
  M code_test.go
atomic/internal/validate/
  M validate.go            top-level parse via ParseInterspersed
  M spec.go, config.go, artifacts.go   subcommand flag sets removed; positionals only
  M *_test.go
atomic/cmd/atomic/
  M cmd_migrate.go         parseMigrateArgs; positional only as --show-log <since>
  M cmd_migrate_test.go
  M cmd_reminder.go        parseReminderAddArgs
  M cmd_reminder_test.go
docs/reference/
  M code-intel.md          flags before or after the positional
docs/spec/
  M follow-ups-folder.md   CLI surface: argument order + change log
  M cli-cobra.md           argument-order section + change log
  M atomic-validate.md     config/bundle take no paths + change log
  M verify-gate-validate.md  config interface without paths + change log
  A cli-trailing-flags.md  this spec
```

## Outline

```
cliutil/args.go
  ParseInterspersed — parse flags in any position; split at first "--"; return positionals in order
  UnexpectedArgs — name the extra arguments and print usage
followups/cli.go
  runClose — exactly one id; extra positionals → exit 2
  runList, runAdd — reject positionals with exit 2
  render/path dispatch — reject any argument with exit 2
serve/serve.go
  parseFlags — interspersed parse; reject a second positional
codeintel/cli/code.go
  positional verbs — interspersed parse; positionals join as before
validate/validate.go
  Run — interspersed top-level parse; subcommands get positionals only; config and bundle reject any
validate/spec.go, config.go, artifacts.go
  runSpec, runConfig, runArtifacts — no flag parsing of their own
cmd_migrate.go
  parseMigrateArgs — interspersed parse; positional allowed only as --show-log <since>
  runMigrate — act on the parsed options
cmd_reminder.go
  parseReminderAddArgs — interspersed parse; text from the joined positionals
```

## Flows

**Close with a trailing reason**

1. User runs `atomic followups close demo --reason "fixed in #12"`.
2. `ParseInterspersed` returns positionals `[demo]` and sets `reason`.
3. `CloseEntry` appends `- <date> demo — "<title>" — fixed in #12` to `CLOSED.md`, deletes `demo.md`, re-renders `INDEX.md`; exit 0.

**Extra positional**

1. User runs `atomic followups close demo typo`.
2. `ParseInterspersed` returns `[demo typo]`.
3. `runClose` prints `atomic followups close: unexpected argument(s): ["typo"]` and the usage line to stderr; exit 2; nothing changes on disk.

**Terminator**

1. User runs `atomic followups close -- demo --reason x`.
2. Positionals are `[demo --reason x]`; `runClose` exits 2.

**Reminder with a trailing due**

1. User runs `atomic reminder add "check the build" --due 2026-11-01T09:00:00Z`.
2. `parseReminderAddArgs` returns text `check the build` and sets `due`.
3. `reminder.Add` stores both; the reminder id prints; exit 0.

**Path given to a whole-repo validator**

1. User runs `atomic validate config docs/spec/x.md`.
2. The top-level parse leaves `[docs/spec/x.md]` after `config`.
3. `validate` prints `atomic validate config: unexpected argument(s): ["docs/spec/x.md"]` and usage; exit 2; no validator runs.

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| A query, symbol, or reminder-text word starting with `-` (`code search foo -bar`, `reminder add fix the -x output`) parses as a flag and errors | Low | The error is loud ("flag provided but not defined"; exit 2 for `code`, exit 1 for `reminder add`); put the words after `--` to keep them literal |
| A `validate` subcommand reads a flag token as a path | Medium | Only the top level parses flags; subcommands receive positionals only. Tests cover a flag before and after the subcommand |
| A script relied on extra positionals being ignored | Low | That script was already losing input silently; exit 2 names the extra arguments |

## Change log

### 2026-10-20 — Initial spec

**What changed:** New spec for honoring or rejecting trailing flags on the verbs listed under Scope.

**Why:** `atomic followups close <id> --reason "<text>"` exited 0 and dropped the reason. Issue #158 fixed the same class for the since-removed `wiki stamp`.
