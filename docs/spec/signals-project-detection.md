# Spec: project detection in `atomic signals`


## Goal


Enrich `## Manifests` bullets in `deterministic-signals.md` with two new annotations: detected framework/project type, and a capped list of runtime dependency names. Add a committed `.claude/project/atomic.toml` for per-repo scan tuning (tree depth, excluded dirs). LLM readers (and humans) can identify project shape and key dependencies in monorepos without reading every manifest.


## Non-goals


- No source-content regex (`FastAPI()` inside `main.py`, etc.). Marker files + manifest deps only.
- No new `## Frameworks` section. Annotations ride existing manifest bullets.
- No user-extensible rule table in v1. Rules live in Go code; new frameworks land via PR.
- No `devDependencies` printed. (They are *used* for detection.)
- No lockfile parsing. Transitive deps are out of scope.
- No transitive framework inference (e.g. "this uses Vite which is used by SvelteKit"). Each rule stands alone.
- No memory-based config. Project scan shape is per-repo, not per-user.


## Success criteria


- [ ] `.claude/project/atomic.toml` is read on every `atomic signals` run; missing file = defaults applied silently; malformed file = exit non-zero with parse error location.
- [ ] `## Manifests` bullets gain `framework=<name>` annotation when a rule matches; multiple manifests in a monorepo each annotate independently.
- [ ] `## Manifests` bullets gain `deps=[a, b, c, ...] (+N more)` showing runtime dependency names from `dependencies` / `[dependencies]` / `require` / `[project.dependencies]`. Capped at 25 names; overflow indicator when truncated. `devDependencies` excluded from output.
- [ ] Workspace-root manifests gain `workspace=true` annotation (npm/pnpm/yarn `workspaces`, Cargo `[workspace]`).
- [ ] At least 61 framework rules registered (10 JS/TS, 11 Python incl. dbt, 5 each for Swift, Go, Rust, Ruby, Java, Kotlin, PHP, C#/.NET).
- [ ] `dbt_project.yml` recognized as a first-class manifest with `name=`, `version=`, `profile=` extraction.
- [ ] Detection precedence: meta-framework > bundler > package-manager-only. Highest-precedence match wins per manifest; orthogonal flags (e.g. Tauri + workspace) coexist.
- [ ] `atomic.toml` supports `[scan] tree_max_depth`, `[scan] exclude_dirs`. Defaults: tree depth 3, no extra excludes.
- [ ] Existing manifest annotations (`name`, `version`, `scripts`, `module`, `go`, etc.) unchanged in field order and format. New fields appended.
- [ ] Test matrix: one fixture per rule under `internal/signals/testdata/frameworks/<rule>/`; table-driven test asserts expected annotation.


## Detection model


Each rule:

```go
type FrameworkRule struct {
    Name        string   // "next.js"
    Manifest    string   // "package.json" — anchor file (must exist)
    Markers     []string // ["next.config.js", "next.config.mjs"] — paths relative to manifest dir
    Deps        []string // ["next"] — dep names in manifest's deps OR devDeps
    Precedence  int      // higher wins; meta-framework=30, bundler=20, mgr=10
    Language    string   // for grouping in tests/docs
}
```

A rule matches at directory D when:

1. `D/<Manifest>` exists, AND
2. At least one of: any `Markers[i]` exists at `D/<marker>`, OR any `Deps[i]` appears in the parsed manifest's runtime *or* dev dependency keys.

For each manifest, the highest-precedence matching rule supplies `framework=<name>`. Workspace detection runs separately and may add `workspace=true` regardless of framework match.


## Output shape


Before:

```
- package.json: name=foo, version=1.2.3, scripts=[build, test]
```

After:

```
- package.json: name=foo, version=1.2.3, scripts=[build, test], framework=next.js, deps=[next, react, react-dom, ...] (+18 more)
- src-tauri/Cargo.toml: name=foo-tauri, version=0.1.0, framework=tauri, deps=[tauri, serde, tokio]
- apps/web/package.json: name=web, version=0.1.0, framework=sveltekit, workspace=true, deps=[...]
```

Annotation order is fixed: existing fields first, then `framework=`, `workspace=`, `deps=`. Absent annotations are omitted (no `framework=none`).


## Configuration: `.claude/project/atomic.toml`


Committed to the repo. Defaults applied silently when file is absent.

```toml
[scan]
tree_max_depth = 3              # int; current hardcoded value
exclude_dirs = []               # list of repo-relative path prefixes; appended to built-in skipPrefixes
```

Parsed via `github.com/BurntSushi/toml`. Unknown keys → log warning to stderr, continue. Malformed TOML → exit 1 with parser error message including line number. Schema validation: unknown keys warned, missing keys defaulted, type mismatches error.

`tree_max_depth` overrides the current `maxDepth = 3` constant in `tree.go`. `exclude_dirs` extends `skipPrefixes` — entries match as path prefix (e.g. `"packages/legacy"`).


## Framework matrix (v1)


55 rules. Each row: `(manifest, markers, deps, precedence)`.

**JS/TS (10)** — manifest `package.json`:

| Name | Markers | Deps | Prec |
|------|---------|------|------|
| next.js | next.config.{js,mjs,ts} | next | 30 |
| nuxt | nuxt.config.{js,ts} | nuxt | 30 |
| sveltekit | svelte.config.{js,ts} | @sveltejs/kit | 30 |
| remix | remix.config.js | @remix-run/dev | 30 |
| astro | astro.config.{js,mjs,ts} | astro | 30 |
| nestjs | nest-cli.json | @nestjs/core | 30 |
| angular | angular.json | @angular/core | 30 |
| react-native | metro.config.js, app.json | react-native | 30 |
| express | — | express | 20 |
| vite | vite.config.{js,ts,mjs} | vite | 20 |

**Swift (5)** — manifest `Package.swift` (markers may also anchor without manifest for Xcode-only projects; flagged in tests):

| Name | Markers | Deps | Prec |
|------|---------|------|------|
| vapor | — | vapor | 30 |
| tuist | Project.swift, Workspace.swift | — | 30 |
| spm-library | — | (none required) | 10 |
| xcode-app | *.xcodeproj, *.xcworkspace | — | 30 |
| fastlane | fastlane/Fastfile | — | 20 |

**Python (11)** — manifest `pyproject.toml` OR `requirements.txt` OR `Pipfile` OR `environment.yml` OR `dbt_project.yml`:

| Name | Markers | Deps | Prec |
|------|---------|------|------|
| django | manage.py | django | 30 |
| flask | — | flask | 30 |
| fastapi | — | fastapi | 30 |
| streamlit | .streamlit/config.toml | streamlit | 30 |
| scrapy | scrapy.cfg | scrapy | 30 |
| airflow | dags/, airflow.cfg | apache-airflow | 30 |
| dbt | dbt_project.yml | dbt-core, dbt-* adapters | 30 |
| jupyter | notebooks/*.ipynb, *.ipynb at manifest dir | jupyter, notebook | 20 |
| poetry | — | `[tool.poetry]` table | 10 |
| pipenv | Pipfile | — | 10 |
| conda | environment.yml | — | 10 |

`dbt_project.yml` is a first-class manifest: parser extracts `name:`, `version:`, `profile:` via top-level line scan (same approach as `pom.xml`/Gradle parsers — no YAML lib dependency). Annotation example: `- analytics/dbt_project.yml: name=analytics, version=1.0.0, profile=warehouse, framework=dbt`.

**Go (5)** — manifest `go.mod`:

| Name | Markers | Deps | Prec |
|------|---------|------|------|
| gin | — | github.com/gin-gonic/gin | 30 |
| fiber | — | github.com/gofiber/fiber | 30 |
| echo | — | github.com/labstack/echo | 30 |
| hugo | hugo.{toml,yaml}, config.toml | — | 30 |
| cobra-cli | — | github.com/spf13/cobra | 20 |

**Rust (5)** — manifest `Cargo.toml`:

| Name | Markers | Deps | Prec |
|------|---------|------|------|
| tauri | src-tauri/tauri.conf.json | tauri | 30 |
| actix-web | — | actix-web | 30 |
| axum | — | axum | 30 |
| bevy | — | bevy | 30 |
| leptos | Trunk.toml | leptos, yew | 30 |

**Ruby (5)** — manifest `Gemfile`:

| Name | Markers | Deps | Prec |
|------|---------|------|------|
| rails | bin/rails, config/application.rb | rails | 30 |
| sinatra | — | sinatra | 30 |
| jekyll | _config.yml | jekyll | 30 |
| hanami | config/app.rb | hanami | 30 |
| rake | Rakefile | rake | 10 |

**Java (5)** — manifest `pom.xml` OR `build.gradle`:

| Name | Markers | Deps | Prec |
|------|---------|------|------|
| spring-boot | — | spring-boot-starter | 30 |
| quarkus | — | io.quarkus | 30 |
| micronaut | micronaut-cli.yml | micronaut-bom | 30 |
| android | app/src/main/AndroidManifest.xml | — | 30 |
| grails | grails-app/ | — | 30 |

**Kotlin (5)** — manifest `build.gradle.kts`:

| Name | Markers | Deps | Prec |
|------|---------|------|------|
| kmp | — | kotlin("multiplatform") plugin | 30 |
| android-kotlin | app/src/main/AndroidManifest.xml | — | 30 |
| ktor | — | io.ktor | 30 |
| compose-multiplatform | — | org.jetbrains.compose | 30 |
| kotlin-jvm | — | kotlin("jvm") plugin | 10 |

**PHP (5)** — manifest `composer.json` OR sentinel files:

| Name | Markers | Deps | Prec |
|------|---------|------|------|
| laravel | artisan | laravel/framework | 30 |
| symfony | bin/console, config/bundles.php | symfony/* | 30 |
| wordpress | wp-config.php | — | 30 |
| drupal | core/, *.info.yml | — | 30 |
| composer | — | (any) | 10 |

**C#/.NET (5)** — manifest `*.csproj` OR `*.sln`:

| Name | Markers | Deps | Prec |
|------|---------|------|------|
| aspnet-core | — | Microsoft.NET.Sdk.Web in csproj | 30 |
| blazor | *.razor | — | 30 |
| maui | <UseMaui>true</UseMaui> in csproj | — | 30 |
| unity | Assets/, ProjectSettings/ | UnityEngine | 30 |
| dotnet-sln | *.sln, *.slnx | — | 10 |


## Checkpoints


| # | Checkpoint | Files/areas | Verifies |
|---|------------|-------------|----------|
| 1 | Add `BurntSushi/toml` dep; create `internal/atomicconfig/config.go` loading `.claude/project/atomic.toml` with defaults + validation | `atomic/go.mod`, `atomic/internal/atomicconfig/` | Unit test: missing file → defaults; valid file → parsed values; malformed → error with line number; unknown keys → warning |
| 2 | Wire config into `signals.go` and `tree.go`: pass `tree_max_depth` and `exclude_dirs` through | `atomic/internal/signals/{signals,tree}.go` | Existing tree test + new test asserting depth override + exclude_dirs prefix skip |
| 3 | Add `internal/signals/frameworks.go` with `FrameworkRule` struct + 55-rule table organized by language | `atomic/internal/signals/frameworks.go` | Compile + lint; table-length assertion |
| 4 | Extend manifest parsers to expose `deps()` (runtime) and `allDeps()` (runtime + dev) on a unified `ParsedManifest` interface; parsers: `package.json`, `Cargo.toml`, `pyproject.toml`, `composer.json`, `pom.xml`, `build.gradle{,.kts}`, `requirements.txt`, `Gemfile`, `go.mod`, `dbt_project.yml` (new) | `atomic/internal/signals/manifests.go` | Unit tests per parser: dep names extracted, devDeps separable; dbt_project.yml: name/version/profile extracted |
| 5 | Implement detection engine: per-manifest, evaluate all rules anchored on that manifest's filename; precedence-pick winner; produce `FrameworkMatch{Name, Workspace}` | `atomic/internal/signals/detect.go` | Table test: each rule fires on its fixture; precedence honored when multiple match |
| 6 | Wire `framework=`, `workspace=`, `deps=[...] (+N more)` into manifest bullet rendering; cap deps at 25 | `atomic/internal/signals/manifests.go` (`ScanManifests` rendering) | Golden test: monorepo fixture with Next.js + Tauri produces expected multi-line output |
| 7 | Create testdata fixtures under `internal/signals/testdata/frameworks/<language>/<rule>/` covering each of the 55 rules + 2 monorepo cases | `atomic/internal/signals/testdata/frameworks/` | All rules detected by table test |
| 8 | Update `docs/spec/atomic-binary.md` § Deterministic signals output with new annotation format; bump `atomic_version` to next minor; update `README.md` | `docs/spec/atomic-binary.md`, `README.md` | Manual review |


## Risks


| Risk | Likelihood | Mitigation |
|------|------------|------------|
| `atomic signals diff` churn when rules added/changed | high | Stability commitment: new rules in minor versions only, removals require major bump; document in `atomic-binary.md` |
| False positives from dep-name matches (e.g. `next` pulled as a tool dep without being a Next.js app) | med | Accept. Cost of one wrong label is low; complex disambiguation isn't worth it. Marker-file rules don't have this problem |
| Dep list inflates signals file in monorepos with large `package.json` files | med | Cap at 25 names + overflow indicator; `devDependencies` excluded from print |
| Parser brittleness on hand-rolled `Gradle`/`pom.xml`/`pyproject.toml` formats | med | Use real TOML parser (`BurntSushi/toml`) for `Cargo.toml`, `pyproject.toml`, `atomic.toml`; keep `pom.xml`/Gradle/Gemfile as line-scanned best-effort; document limitation |
| `atomic.toml` schema drift across versions | low | Unknown keys warn-not-error; document schema in `atomic-binary.md`; version the schema if it grows past trivial |
| Detection table maintenance burden | low | v1 capped at 55 rules with documented "what gets in" criteria (>5% language share, distinct project shape, not a library); PRs gate additions |
| Workspace detection conflates root with member packages | low | Workspace flag only set on the *root* manifest (the one declaring `workspaces`/`[workspace]`); member manifests don't carry it |


## Open questions


None — surfaced concerns from `/atomic-plan` clarify round were resolved before drafting (see preamble in transcript).
