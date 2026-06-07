---
name: atomic-cli-contrib
description: Conventions for editing the atomic CLI and command artifacts in this repo. Auto-fires on phrases like "add a CLI subcommand", "wire a new flag", "prompt the user", "add a doctor check", "add a doctor repair", "edit cmd/atomic/main.go", "extend claudeinstall", "add an internal package", "use huh", "add a command", "create a new verb", "add a partial", "render templates", "edit a command", "edit commands/", "create a new command", "edit an agent", "edit agents/", or "add an agent". Contributor-only — never bundled, never installed.
user-invocable: false
---


# atomic-cli-contrib


Project-local skill for working *on* the `atomic` CLI in this repo. Captures conventions that emerged from the doctor, validate, and config work, plus the prompt-layer extraction. Read this before adding subcommands, flags, prompts, or new internal packages.


This skill is contributor-scope only. It lives under `.claude/skills/` and is auto-loaded for sessions in this repo. It is not bundled (see `atomic/internal/bundlemirror/mirror.go` — only `skills/atomic-*/` at the repo root ships).


## 1. Interactive prompts go through `internal/prompt/`


- **Single surface.** Every interactive prompt — install flows, doctor `--fix`, future `atomic config`, anything new — calls `internal/prompt.Confirm` or `internal/prompt.Select[T]`. No direct `huh.*` calls outside the prompt package. No `bufio.Scanner` prompters.
- **Why.** One swap point when `huh` changes API. Consistent TTY detection. Consistent abort handling.
- **Sentinels.** Callers branch on `errors.Is(err, prompt.ErrNonInteractive)` (no TTY → skip path) and `errors.Is(err, prompt.ErrAborted)` (Ctrl+C → distinct from "No"). Never collapse abort into decline.
- **Doctor adapter.** The doctor's `Prompter` interface lives separately so the `--fix` loop can have its own decision shape (`DecisionYes`/`DecisionNo`/`DecisionSkip`/`DecisionAbort`). The adapter (`doctor/stdin_prompter.go`) translates from `internal/prompt` errors. Mirror the adapter pattern if you build a new prompt-consuming subsystem.


## 2. Testable seams via function-field structs


- **Pattern.** For any subsystem that calls external collaborators (filesystem, network, prompt, time), define a struct of function fields:

    ```go
    type MyStep struct {
        Classify  func(scopeRoot, want string) (Result, error)
        Write     func(scopeRoot, value string) error
        Confirm   func(title, desc string, def bool) (bool, error)
        Logger    io.Writer
        AssumeYes bool
    }
    ```

    Default factory wires production deps. Tests inject stubs.

- **Public entry points.** Expose `*WithStep` variants alongside the bare verb: `Install`, `InstallWithStep`, `Update`, `UpdateWithStep`. CLI dispatch always uses the seam-aware form (`InstallWithStep(target, ..., step)`) so flags can plumb through.

- **Why.** Unit tests stay TTY-free, network-free, filesystem-free where possible. Seam-stubbed tests catch dispatch bugs but cannot catch path-resolution bugs (see §3 for the failsafe — the `scope-root` family of mistakes survives seam tests trivially).


## 3. End-to-end tests against real paths


Seam-stubbed tests prove dispatch logic, not path resolution. For anything that reads or writes user state (`~/.claude/...`, `~/.config/atomic/...`), add at least one test that:


- Uses `t.Setenv("HOME", t.TempDir())` or equivalent.
- Writes a fixture at the path production would actually read.
- Calls the production entry point (not the seam).
- Asserts on the real file on disk.


The scope-root double-prepend class of bug (caller passes `~/.claude` as `scopeRoot`, callee internally prepends `.claude/` again, real production runs miss the user's actual file) passes seam-stubbed tests trivially because the test fixture is written to the buggy resolved path. A `t.Setenv("HOME", ...)` end-to-end test is the only thing that catches it.


## 4. `scopeRoot` is the parent of `.claude/`, not `.claude/` itself


The `outputstyle.*` and `settingsjson.*` helpers expect `scopeRoot` to be the directory that *contains* `.claude/`. They internally resolve `<scopeRoot>/.claude/settings.json`.


- Default user scope: `scopeRoot = $HOME`, settings at `$HOME/.claude/settings.json`.
- CLI flag `--target` defaults to `$HOME/.claude` (the `.claude/` dir itself). Callers must pass `filepath.Dir(target)` as `scopeRoot`. Three call sites had this wrong before the fix; check any new caller.
- `hooks.Install(repoRoot, home)` follows the same convention — pass `home`, not `home + "/.claude"`. Mirror that precedent.


## 5. axiom interactions for new CLI surfaces


When adding a verb, flag, or repair:


| Axiom | What it means for CLI work |
|-------|----------------------------|
| 1 — cohesion-bounded scope | A new subcommand can touch many files in one slice (cmd dispatch + internal package + tests + spec). Don't artificially split. |
| 2 — memory-first | New tunable defaults (thresholds, depth limits, sizes) go to user auto-memory. No `.atomicrc`, no env vars for tunables. Argument-level vars stay on the flag set. |
| 3 — destructive ops require explicit per-item confirm | Any new repair, mutation, or delete prompts. The doctor `Repair` loop already prompts uniformly; do not add a per-check bypass. `--yes` is *explicit user consent* (different from non-interactive — see §1). |
| 4 — plain-text indexed selection over multi-select UI | Lists of 4+ items use a printed numbered list + typed input syntax (`1 3 5`, `1-3`, `all`, `none`). `huh.MultiSelect` is for fixed small choice sets only. |
| 5 — skills auto-fire; commands explicit | Doesn't apply to atomic CLI verbs directly — they're explicit binary subcommands by nature. But if adding a Claude Code skill that wraps a CLI verb, the skill's description must describe natural-language triggers, not negate them. |


## 6. New internal packages


- Path: `atomic/internal/<package>/`.
- Tests co-located: `<package>_test.go` next to `<package>.go`. Fixtures in `<package>/testdata/` if needed.
- Public surface kept small. Don't export internal helpers that only one caller uses — keep them unexported.
- Core third-party Go deps: `gopkg.in/yaml.v3` (frontmatter), `github.com/tailscale/hujson` (JWCC settings.json), `github.com/BurntSushi/toml` (config), `github.com/charmbracelet/huh` (prompts; transitively pulls bubbletea + lipgloss + friends). Add a new dep only when there's a concrete capability gap and the dep is well-maintained. Document the why in the commit message.


## 7. Doctor: adding a check


- File: `atomic/internal/doctor/checks_<name>.go` (production wrapper + `Run<Name>With(scopeRoot)` testable inner function).
- Register in `doctor.go`'s `categories` slice. Pick a stable index (don't reorder existing entries — tests assert on indices).
- Severity defaults: `PASS` / `WARN` / `SKIP` from `Result.Severity`. Reserve `FAIL` for things that prevent atomic itself from running.
- `--fix` repair: add to `repairPlan` + `applyRepair` switch in `fix.go`. Implement `default<Name>Repair` in `fix_impls.go`. Use the existing `*RepairFn` global + `Set*RepairFn` test seam. Repair runs only after the loop's per-item `Confirm` returns `DecisionYes`.
- Spec: append a `## Change log` entry to `docs/spec/atomic-doctor.md` when adding or changing a check. See "Spec files are append-mostly" in `CLAUDE.md`.


## 8. CLI dispatch (cmd/atomic/main.go)


- One `runX` function per top-level verb. Flag parsing inside `runClaude` / `runDoctor` / etc.
- Subverb dispatch (e.g. `claude install` vs `claude list`) uses a string switch on `args[0]`. Lowercase, hyphen-separated.
- Flags use stdlib `flag.NewFlagSet(verb, flag.ContinueOnError)`. No `cobra` or `urfave/cli` — too much surface for the size we're at.
- Bool flags that change destructive behavior (`--yes`, `--force`): default `false`, name them positively, scope tightly. Don't broaden a `--yes` flag's effect across multiple prompts in one CL — convention is one-prompt-one-flag, not omnibus consent.
- Error to stderr, exit non-zero. Use `fmt.Fprintln(os.Stderr, ...)` not `log.Println` (no timestamps in CLI output).
- Post-install "next steps" text: keep it short, mention only actions the user must still take. Anything the install just did automatically should NOT be in next steps — that's a stale-doc trap, and stale "next steps" lines outlive the code that obsoletes them by multiple reviews.


## 9. Build hygiene


- `make -C atomic build` outputs to `atomic/bin/atomic` per the Makefile.
- `go build ./cmd/atomic` (run from inside `atomic/`) drops a binary at `atomic/atomic`. Gitignored at the repo root (`/atomic/atomic`, `/atomic/bin/`, `/atomic/tmp/`).
- `go build` from the repo root targeting `./atomic/cmd/atomic` drops the binary at the repo root — won't happen in normal workflow but worth knowing.
- The bundle regenerator runs on every commit that touches a source artifact (`agents/`, `commands/`, `skills/`, `output-styles/`, `rules/`, `CLAUDE.md`). Pure `atomic/` changes do NOT trigger a regen. See `.githooks/pre-commit`.


## 10. Command & agent artifact templates — `templates/` is the only edit path


Both `commands/` AND `agents/` are **fully generated** from `templates/` via `make render`. Never edit `commands/<name>.md` or `agents/<name>.md` directly — the change is overwritten on the next render. The rendered kinds and their order are defined in `templaterender.renderedKinds` (`["commands", "agents"]`).


**Source of truth:**

- `templates/commands/<name>.md` — verb-specific orchestration for that command.
- `templates/agents/<name>.md` — agent body. Most are self-contained; builder + surgeon pull verbatim-shared blocks via `{{ template "agent-*" . }}`.
- `templates/shared/<name>.md` — reusable partials included via `{{ template "<name>" . }}`. One shared pool for both kinds.


**Partial taxonomy:**

| Kind | Examples | Description |
|------|---------|-------------|
| Command big partials | `commit-flow`, `pr-flow`, `merge-flow`, `squash-flow`, `push-flow` | Entire flow bodies consumed by one or more command templates |
| Command small partials | `doc-impact`, `doc-impact-why`, `signals-gate`, `base-resolution`, `worktree-cleanup-prompt`, `git-safety` | Fragments embedded inside big partials |
| Agent partials (`agent-` prefix) | `agent-tdd-signals`, `agent-signals-output`, `agent-shared-rules` | Blocks shared verbatim across builder + surgeon (TDD workflow steps 3-4, signal output format, discipline rules) |


**Adding a new command or agent:** drop `templates/commands/<name>.md` or `templates/agents/<name>.md`, run `make render`. Never create the output file directly.


**Removing a command or agent:** delete BOTH the template AND the rendered output. An orphan output file without a matching template causes `make render` to halt with a non-zero, kind-aware error.


**Partial design rules:**

- Pure fragments only. No `dict` function, no `{{ if }}` conditionals, no variant flags inside partials. When two consumers diverge by one word (e.g. builder/surgeon git-state rule), generalize the wording so the partial stays verbatim-shared, or keep that bullet inline per-agent.
- Optional sub-fragments are their own micro-partials (e.g. `doc-impact-why` is separate from `doc-impact` so callers can include one without the other).
- To verify: `make render && git diff --exit-code commands/ agents/` must exit 0 after any template edit.


**Render workflow:**

    make render                                # regenerate commands/ and agents/ from templates/
    git diff --exit-code commands/ agents/     # assert no stale output

The pre-commit hook auto-runs `make render` and re-stages `commands/` and `agents/` whenever any `templates/` file is staged.


## 11. Manual exercisers in `tmp/`


- `tmp/` is gitignored except for the two `.gitkeep` files.
- Real bugs that hide behind seam-stubbed tests surface fast under a sandboxed `HOME` exerciser. Pattern: `mkdir -p tmp/sandbox/.claude`, `HOME=$PWD/tmp/sandbox tmp/build/atomic ...`, then assert on the file the CLI was supposed to write. Three scope-root bugs were found this way that the unit tests missed.
- Keep these scripts cheap to write and re-runnable. Don't commit them (gitignored is correct) but reference them in PR descriptions when they helped find a bug.


## 12. Library references


### `charmbracelet/huh` — interactive forms / prompts


- Repo: <https://github.com/charmbracelet/huh>
- Pkg docs: <https://pkg.go.dev/github.com/charmbracelet/huh>
- Bubbletea (underlying TUI runtime): <https://github.com/charmbracelet/bubbletea>
- Lipgloss (styling): <https://github.com/charmbracelet/lipgloss>
- Showcase / patterns: <https://github.com/charmbracelet/huh#examples>


How we use it today (`atomic/internal/prompt/prompt.go`):


- `huh.NewConfirm().Title(t).Description(d).Affirmative("Yes").Negative("No").Value(&result)` — binary y/n. Wrapped as `prompt.Confirm`.
- `huh.NewSelect[T]().Title(t).Description(d).Options(opts...).Value(&result)` — single pick from a typed list. Wrapped as `prompt.Select[T]`. Constraint: `T comparable` (huh's restriction, not ours).
- `huh.NewOption[T](label, value).Description(desc)` — option constructor.
- `huh.NewForm(huh.NewGroup(field, field, ...)).Run()` — runs the form. We currently use one field per form; group is the structural unit for multi-field future.
- `huh.ErrUserAborted` — Ctrl+C sentinel. Mapped to `prompt.ErrAborted` in `defaultRunConfirm`.


Likely next uses (when `atomic config` lands or new prompts arrive):


- `huh.NewInput().Title(t).Value(&str).Validate(fn)` — free-form text input with validation. Use for "what's your name" / "what's your project slug" style fields.
- `huh.NewMultiSelect[T]().Options(...).Value(&[]T)` — multi-select. Reserve for *fixed small choice sets* only (per axiom 4 — use plain-text indexed selection for ≥4 unbounded items).
- `huh.NewText().Title(t).Value(&str).Lines(N)` — multi-line input. Probably overkill for atomic; flag if proposed.
- `huh.NewGroup(...).WithHideFunc(func() bool { return !cond })` — conditional reveal across fields. The reason huh was chosen — once we have grouped config wizards this pays off.
- `.Validate(func(s string) error { ... })` — per-field validation. Return `errors.New("...")` to keep the user in the field with an error message.
- `huh.NewForm(...).WithTheme(huh.ThemeBase())` — strip the default rounded-border styling if it clashes with atomic minimalism. Tracked as an open question for the look-and-feel pass.


Gotchas:


- huh switches the terminal to alt-screen / raw mode. In non-TTY environments `Form.Run()` errors immediately; our `internal/prompt` package detects this before reaching huh and returns `ErrNonInteractive` instead.
- `NewConfirm` has no explicit `.Default(bool)` builder. Set the bound variable to the default *before* `Form.Run()` and rely on huh's `PointerAccessor` reading through the pointer at render time. Revisit on every huh upgrade — if a future huh version resets bound values during form init, every confirm silently becomes `false`. (Tracked in `.claude/project/followups.md` as `install-output-style-F-2`.)
- huh re-renders on every keystroke. Don't pass huge `Description` strings — keep them to one line where possible.


### `tailscale/hujson` — JWCC (JSON with comments + commas)


- Repo: <https://github.com/tailscale/hujson>
- Pkg docs: <https://pkg.go.dev/github.com/tailscale/hujson>
- JWCC spec context: <https://nigeltao.github.io/blog/2021/json-with-commas-comments.html>


How we use it (`atomic/internal/hooks/hooks_hujson.go`, `atomic/internal/outputstyle/outputstyle.go`, `atomic/internal/settingsjson/settingsjson.go`):


- `hujson.Parse(b []byte) (*hujson.Value, error)` — parse JWCC bytes into an AST. The AST preserves comments, trailing commas, key order, blank lines.
- `hujson.Standardize(b []byte) ([]byte, error)` — JWCC → strict JSON bytes (comments stripped, trailing commas removed). Use this *only* when handing bytes to `json.Unmarshal`. Never write the standardized bytes back to the user's file — that destroys their formatting.
- `Value.Pack() []byte` — serialize the AST back to JWCC bytes, preserving everything the parse captured. **This is the surgical-merge primitive.**
- `Value.Value` — the underlying `hujson.ValueTrimmed` payload (an `Object`, `Array`, `Literal`, `String`, etc.).
- `Object.Members []ObjectMember` — ordered list of key/value pairs. Mutate in place to add/remove/replace fields.
- `ObjectMember.Name` (key) and `.Value` (value), both as `Value` (so they carry their own before/after comment metadata).


Our reusable helpers (`atomic/internal/settingsjson/settingsjson.go`):


- `EnsureObject(v *hujson.Value) (*hujson.Object, error)` — coerce a `Value` to an `Object` or fail loudly.
- `FindMember(obj *hujson.Object, name string) (int, bool)` — locate a top-level key by name.
- `RemoveMember(obj *hujson.Object, name string)` — drop a key in place.
- `ParseJSONString(raw []byte) (string, error)` — decode a JSON-encoded string literal (e.g. an `ObjectMember.Value` that holds a string).
- `TopLevelKeys(v *hujson.Value) ([]string, error)` — capture top-level key order; useful as the input to an iron-rule guardrail (snapshot keys before mutation, compare after, refuse to commit the rename if any original key is missing).


Likely next uses:


- Reading any new user-config file that allows comments. Today only `~/.claude/settings.json` is JWCC; future config surfaces should follow.
- Mutating a nested key (e.g. `permissions.allow`). The same AST pattern applies — walk down via `Object.Members`, then mutate the target member. Don't re-marshal the whole tree.
- Inserting a new top-level key while preserving order: append to `obj.Members`. Inserting at a specific index: slice splice.


Gotchas:


- `hujson.Parse` returns a *pointer* to `Value`. Modifications mutate in place.
- `Value.Pack()` produces canonical JWCC, not byte-identical input. Whitespace around tokens may shift; comment text and trailing commas survive. The iron rule guards against *key loss*, not *whitespace stability*.
- `json.Unmarshal` does NOT accept JWCC. Always `Standardize` first if you need a Go struct. Pattern: `hujson.Parse` for validation (returns an AST), then `hujson.Standardize` + `json.Unmarshal` to extract typed values.
- Comments belong to the *following* token. When removing a member, its `BeforeExtra` comment goes with it. Removing a key with an important comment loses the comment — flag this if it ever bites.


## Common mistakes to avoid


1. **Passing the `.claude/` dir as `scopeRoot`.** Always one level higher. See §4.
2. **Adding a new prompt with direct `huh.*` calls.** Route through `internal/prompt`. See §1.
3. **Marshaling a Go struct over the user's `settings.json`.** Wipes their comments, plugin keys, custom hooks. Use the `internal/settingsjson` AST helpers. See `atomic/internal/outputstyle/outputstyle.go:Write` for the surgical merge pattern.
4. **Trusting seam-stubbed tests for path-resolution coverage.** They won't catch scope-root bugs. Add at least one `t.Setenv("HOME", ...)` end-to-end test per new state-touching subsystem. See §3.
5. **Broadening `--yes` to silence unrelated prompts.** The flag's contract is "auto-accept *this specific* prompt." A future PR that conflates non-interactive (no TTY) with `--yes` (user consent) breaks the iron rule from axiom 3.
6. **Forgetting to delete stale "next steps" text** when the install step starts doing the thing automatically. The CLI's own user-facing output is documentation too — keep it synchronized with the code.


## Related


- `docs/spec/atomic-doctor.md` — doctor check + repair conventions.
- `docs/spec/atomic-state-and-config.md` — TOML config + state directory contract.
- `docs/spec/atomic-validate.md` — bundle parity + cross-reference linting.
- `.claude/rules/authoring/axioms.md` — the five design axioms this skill cross-references.
- `CLAUDE.md` § "Subagents available for dispatch" — when to delegate CLI work to `atomic-builder` vs `atomic-surgeon`.
