# OKF alignment for wiki + serve


## Goal


Align the wiki producer and `atomic serve` consumer to Google's Open Knowledge Format (OKF v0.1) where it adds value for a software-dev/git tool: typed concept frontmatter that drives serve's graph coloring + a type filter, standard bundle-relative cross-links that serve renders, a `resource` link in the rail, and an OKF-conformant realm `index.md` listing.


## Non-goals


- No `log.md` — git history is the update log.
- No restructuring of `wiki/repos/<name>.md` pages or their relative source links (their targets are source files, not bundle concepts).
- No changes to signals files inside code repos.
- No central `type` registry (OKF types are producer-defined).
- No removal of serve's `[[…]]` wikilink parser (kept for back-compat tolerance).
- No `okf_version` frontmatter on `index.md` (serve strips it; collides with the managed-block writer).
- No `type: Repo Summary` frontmatter on repo pages this round (serve path-fallback colors them; opt-in documented for later).


## Success criteria


- [ ] serve resolves a standard bundle-relative `[text](/path.md)` link to an in-shell navigable page/file href when the target exists under the served root (no longer treated as filesystem-absolute/broken); relative and `[[…]]` links still resolve.
- [ ] serve `/graph/data` emits a real per-node `type` (short lowercase `repo`/`concern`/`knowledge`/`bucket`/`external`/`page`) via a hybrid resolver: frontmatter `type` (title-case mapped to short class) → path-convention → `page`/`external`. Applied at every node-emission site that currently hardcodes `page` (global graph + rail mini-graph / local subgraph).
- [ ] A served realm shows graph nodes colored by type in the browser (existing CSS selectors fire, no CSS change required beyond the legend).
- [ ] serve exposes a type filter/legend that toggles node visibility per type.
- [ ] serve renders a frontmatter `resource:` URL as a clickable link in the right-rail Properties panel.
- [ ] The signals-inferrer agent (bucket-synthesis B3) writes `type: Knowledge` + `description:` on knowledge pages; cross-links between concepts use bundle-relative `[text](/path.md)`. Agent-written keys survive `atomic wiki stamp`.
- [ ] `/refresh-wiki` writes `type: Concern` + `description:` on concern pages it authors.
- [ ] Realm `index.md` `## Members` is an OKF §6 listing: `- [Title](url) - description` entries, alongside the retained `<wiki-scan>` machine block.
- [ ] `make render` + `make -C atomic bundle` parity holds; `go test ./...`, `go vet`, `gofmt -l` clean; VitePress `docs:build` clean.
- [ ] Artifact checklist done: CLAUDE.md, README, docs/reference (serve + wiki-workflow), `/atomic-help` router updated; `/atomic-help` MISSING-scan clean; signals refreshed.
- [ ] PR carries before/after serve screenshots (or documented HTML-snapshot fallback).


## Approach


Hybrid type resolution (frontmatter → path-convention → default) on the serve side; producer model writes typed frontmatter; standard markdown links throughout — see `docs/design/okf-alignment.md`.


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | serve: resolve leading-slash `/path.md` as bundle-root-relative before the `IsAbs` external fallback | `atomic/internal/serve/graph.go` (`resolvePageHref`) + test | atomic-implementer (feature) | ~2 | test: `/foo.md` under root → in-shell page href; relative + `[[…]]` unaffected |
| 2 | serve: hybrid node-`type` resolver + mapping; populate at all hardcoded-`page` emission sites | `atomic/internal/serve/graphoverlay.go` (`buildCytoElements`, `buildLocalSubgraph`), `graph.go` (resolver helper) + test | atomic-implementer (feature) | ~3 | test: `type: Knowledge` → `knowledge`; `wiki/repos/x.md` path → `repo`; unknown → `page`; rail mini-graph path typed too |
| 3 | serve: type legend/filter UI + `resource` rendered as rail link | `atomic/internal/serve/templates/layout.html`, `rail_handler.go`, `assets/app.css` + test | atomic-implementer (feature) | ~4 | filter toggles node visibility by type; `resource:` URL renders as `<a>` (rail test) |
| 4 | producer: signals-inferrer B3 emits `type: Knowledge` + `description` + bundle-relative concept links; W4 repo-summary output unchanged | `agents/atomic-wiki-inferrer.md` + `make render`/`make bundle` | atomic-implementer (feature) | ~3 | prompt declares the contract; bundle parity; render+bundle committed |
| 5 | producer: realm `index.md` `## Members` → OKF §6 listing with descriptions; concern pages get `type: Concern` | `atomic/internal/wiki/wiki.go` (`buildMembersSection`), `commands/refresh-wiki.md` + test | atomic-implementer (feature) | ~3 | test: Members entries carry ` - <description>` in `- [Title](url)` form; refresh-wiki authors `type: Concern` |
| 6 | docs + artifact wiring | `docs/spec/okf-alignment.md`, `docs/reference/serve.md`, `docs/reference/wiki-workflow.md`, `CLAUDE.md`, `README.md`, `templates/commands/atomic-help.md`, signals refresh, render+bundle | atomic-implementer (feature) | ~7 | `/atomic-help` MISSING-scan clean; render+bundle parity; docs:build clean |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| Type-string mismatch (`Knowledge` ≠ `knowledge`) silently no-ops coloring | Med | Explicit long→short map in the resolver; both resolution paths converge on short lowercase; CP2 test asserts the mapping. |
| Multiple node-emission sites leave rail mini-graph mono-colored | Med | CP2 fixes `buildCytoElements` + `buildLocalSubgraph` through one shared resolver; test the local path. |
| Producer title-case `type` lands before serve mapping exists | Med | Sequence: CP1–CP3 (serve) before CP4–CP5 (producer); producer change is forward-compatible via path-fallback. |
| Agent-written `type` lost in `stamp` round-trip | Low | `updateFrontmatterKey` parses→re-emits all keys (alphabetically); agent keys survive. CP4/CP5 verify a stamped page retains `type`. |
| Headless Cytoscape render fails for screenshots | Med | Chrome headless `--virtual-time-budget` to let ELK layout settle; fall back to rendered-HTML snapshots + documented repro, stated in PR. |


## Change log


<!-- empty on creation; first entry on first post-approval amendment -->
