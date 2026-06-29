# CLI Cobra migration

## Goal

Migrate the `atomic` CLI from a hand-rolled `flag`/`switch` dispatch tree to [Cobra](https://cobra.dev), then derive `[]cliusage.Command` by walking the Cobra root command tree so the A1 artifact-citation linter keeps working without logic changes.

## Non-goals

- No new commands, no new flags, no behavior changes — pure dispatch migration.
- No changes to the A1 linter logic in `validate/artifacts.go`.
- No changes to help text content (descriptions must match the cliusage slice exactly).
- No changes to `codeintel/cli/code.go` or `wiki/action.go` internals — only their entry points are re-wired to Cobra sub-commands.
- No bundled artifact changes (commands/, agents/, CLAUDE.md) in this workstream.

## Success criteria

1. `go test ./...` passes with no regressions.
2. `atomic validate artifacts` (A1 linter) still passes on the full artifact set — `TopLevelVerbs()`, `LookupByPath()`, and `cmd.Flags` consumers in `validate/artifacts.go:39,180,192,231` all receive correct data from the derived `[]cliusage.Command`.
3. `atomic <verb> --help` produces output for every one of the 56 currently-registered command paths (verified by a table-driven test that calls `cobra.Command.Find` for each path and asserts non-nil).
4. `atomic --help` top-level output lists all top-level verbs (currently 16 in the switch at `main.go:95`).
5. `RenderCommandsBlock` is removed from `cliusage.go`; Cobra owns help rendering.
6. No manual duplication: the single call to `deriveCommands(rootCmd)` populates the `commands` slice that `Commands()`, `LookupByPath()`, and `TopLevelVerbs()` read.

## Approach

Walk the Cobra root command tree to produce `[]cliusage.Command` (design decision row 4 in [docs/design/signals-wiki-unification.md](../design/signals-wiki-unification.md)); the A1 linter's data source is repointed, its logic is untouched.

## Checkpoints

| # | Checkpoint | Files / areas | Agent | Est. files | Verifies |
|---|------------|---------------|-------|-----------|----------|
| 1 | Cobra root + top-level verbs | `atomic/cmd/atomic/main.go` — replace 16-case switch (`main.go:95`) and `fs.Usage` block (`main.go:38–45`) with a Cobra root command + 16 top-level `*cobra.Command` stubs; wire `--repo`, `--version`, `--no-update-check` as persistent/global flags; keep `runXxx` call-throughs identical | implementer | 1 | `atomic --help` shows all 16 verbs; `go test ./...` green |
| 2 | Port nested `code` subcommands | `atomic/internal/codeintel/cli/code.go` — replace 11-case switch (`code.go:70`) with 11 Cobra sub-commands under a `code` parent; route each to its existing `runXxx` handler | implementer | 1 | `atomic code --help` lists all 11 verbs; existing code tests green |
| 3 | Port nested `wiki` + `bucket` subcommands | `atomic/internal/wiki/action.go` — replace 6-case switch (`action.go:36`) and 4-case `bucket` switch (`action.go:246`) with Cobra sub-commands under `wiki` and `wiki bucket` parents | implementer | 1 | `atomic wiki --help`, `atomic wiki bucket --help` list correct verbs; wiki tests green |
| 4 | `cliusage` derivation + A1 repoint | `atomic/internal/cliusage/cliusage.go` — add `deriveCommands(root *cobra.Command) []Command` that walks the Cobra tree recursively and maps each leaf to `Command{Path, Args, Flags, Description}`; populate the `commands` var by calling it from an `init()` or a `SetRoot(root)` setter called from `main()`; remove `RenderCommandsBlock` | implementer | 1 | `Commands()`, `LookupByPath()`, `TopLevelVerbs()` return correct data; A1 linter passes |
| 5 | Help rendering | `atomic/cmd/atomic/main.go` — remove the manual `fs.Usage` block that called `cliusage.RenderCommandsBlock`; confirm Cobra's built-in help is the only path; add a table-driven test asserting `rootCmd.Find(path)` returns non-nil for all 56 paths | implementer | 2 | SC3 + SC5 |
| 6 | Remove old switch scaffolding | `atomic/cmd/atomic/main.go` — delete the `flag.FlagSet`, the `switch args[0]` block, and any dead helper code; `atomic/internal/cliusage/cliusage.go` — delete `RenderCommandsBlock` if not already removed; run `go vet ./...` | implementer | 2 | `go vet` clean; `go test ./...` green; SC1–SC5 all hold |

## Risks

| Risk | Likelihood | Mitigation |
|------|-----------|------------|
| A1 linter breaks if `deriveCommands` misses a verb, flag, or nesting level | Medium | SC2 requires running `atomic validate artifacts` against the full artifact set; add a unit test comparing `Commands()` output length (must equal 56) and every `Path` against the prior hard-coded slice |
| `Args` hints (e.g. `<query>`, `[pattern]`) are not present in Cobra's flag metadata — derivation must source them separately | High | Store `Args` in each `cobra.Command` via `Annotations["args_hint"]`; `deriveCommands` reads the annotation; enforce the convention in CP4 |
| `--no-update-check` pre-scan (`scanNoUpdateCheck`) must survive Cobra's arg parsing | Low | Retain `scanNoUpdateCheck` as a pre-pass on `os.Args` before `rootCmd.Execute()`; Cobra's persistent-flag `--no-update-check` registration is for `--help` documentation only, mirroring the current pattern |
| Cobra's default `--help` / `-h` flag conflicts with any existing `-h` short flag | Low | Audit all 56 `Flags` entries for `-h`; none currently use it |
| 3-level nesting (`wiki bucket <sub>`) requires careful parent/child wiring in Cobra | Low | CP3 explicitly covers this; test with `atomic wiki bucket --help` |
| Background update goroutine timing — `bgUpdateCh` select block at `main.go:138` — must survive the Cobra `Execute()` call replacing the switch | Low | Move the banner select block to a `PersistentPostRunE` on the root command or retain it after `rootCmd.Execute()` returns |

## Change log

(none)
