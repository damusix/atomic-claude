# Serve realm UX: member pick, non-git roots, newest-first, provenance


## Goal


`atomic serve` remembers which repo the reader picked without carrying it in the URL, aggregates a root that is not a git repository into Plans as one checkout, opens a plan file at its newest version, names the realm root by the realm's name, says in the top bar which repo and which checkout an open plan file comes from, and lets a bundle's HTML mock run its own scripts inside the sandbox (#234).


## Non-goals


- Moving `?at=` (version selection) out of the URL.
- A realm-nav group for the realm root's own `docs/` tree.
- Any server read of the cookie; no server-side session.
- Consolidating the existing per-component `/api/nav` fetches in `TopBar`, `NavTree`, and `About`.
- Changing which version supplies a row's title and description (`representativeVersion` in `plans.go` is untouched).
- Persisting the pick across hosts (`localhost` and `127.0.0.1` are different cookie hosts).
- Running scripts for HTML outside a scratchpad bundle; the raw route serves only bundle files.
- Granting a bundle frame `allow-same-origin`, `allow-forms`, or `allow-popups`.
- Resetting `?at=` on a member switch. No member picker renders on a `/plans/:slug/*` route and `openSlug` never carries `at`, so a version selection cannot follow a member switch except through browser history, where the held-selection rule re-resolves it against the new member's own versions.


## Success criteria


**Aggregator**

- [ ] A root where `git worktree list --porcelain` fails aggregates as one checkout: id `checkoutID(root)`, branch `""`, `isMain` false, `Created` nil. Its `docs/design/*.md`, `docs/spec/*.md`, and scratchpad bundles each appear with exactly one version or bundle attributed to that checkout.
- [ ] `planBundle` carries `path` (the checkout's display path, per `checkoutDisplayPath`) and `outsideRoot`, mirroring `planCheckout`.
- [ ] Every existing multi-worktree Plans test still passes unchanged.

**Member store**

- [ ] One module-level store (`useSyncExternalStore`, the pattern `components/code-modal/store.ts` already uses) holds `currentMember`. No frontend source reads or writes a `member` search param; the only remaining `member=` strings are server fetch URLs (`/plans`, `/code/schema`, `/code/node`, `/code/{callers,callees,impact}`, the reindex route).
- [ ] The store's identity is `<scope>:<name>` from one `GET /api/nav` per page load. Until that resolves, `ready` is false and every consumer that fetches by member holds its fetch.
- [ ] Persistence is `document.cookie`, name `atomic-member`, value `encodeURIComponent(JSON.stringify(map))`, attributes `path=/; max-age=31536000; SameSite=Lax`. Reading a missing or malformed cookie yields `""`. All cookie access is wrapped so a missing `document` never throws.
- [ ] Only `setMember` writes the cookie, and it rewrites only the current identity's entry, preserving other entries. A page whose member list does not contain the stored member renders its own fallback (Graph: first member; Schema: same rule) and does not write.
- [ ] The stored value is the member's `prefix`, the one identifier both `/api/plans/members` and `/code/graph/members` report; `GET /api/plans?member=` resolves it by prefix. Plans, Graph, and Schema therefore write the same value for the same repo, and the top-bar crumb shows what the pickers show.
- [ ] The code graph mounts into the container React keeps: the element handed to the engine's `mount` is attached to the document, in the fallback case (stored member absent from Graph's list) as much as the direct case.
- [ ] The rail's Plans link stays `/plans`; landing there renders the stored member.
- [ ] `usePlansScope` no longer exposes `member` or `setMember`, and `scopedSearch` is removed; `plansHref()` returns `/plans`; `slugHref()` carries only `at`; `openSlug` navigates to `/plans/<slug>`.

**Labels and default version**

- [ ] In every picker (Plans, Graph, Schema) the member with empty prefix renders the realm's `name` from the store; `(local)` appears nowhere in `frontend/src`.
- [ ] `resolveDocVersion(doc, undefined)` returns the version with the newest `mtime`; a held `at` that resolves still wins; `held` semantics are unchanged; the merged version's `isMain`, label, and filled dot are unchanged.

**Provenance**

- [ ] On `/plans/:slug/*` the slug view publishes the resolved checkout `{ branch, path, outsideRoot }` of the file on screen to a module store and clears it on unmount.
- [ ] In realm scope the top bar renders `plans › <member label> › <slug> › <file>` where the member crumb links to `/plans`; in repo scope the member crumb is absent.
- [ ] After the file crumb the top bar renders `<branch> · <path>`; an empty branch renders `<path>` alone; `outsideRoot` renders the absolute path as given. Off the plans route nothing is rendered.
**Bundle mocks**

- [ ] A bundle file of kind `html` renders in an iframe whose `sandbox` attribute is exactly `allow-scripts`, so the document runs its own scripts in an opaque origin.
- [ ] `GET /api/plans/page?…&raw=1` sends `Content-Security-Policy: sandbox allow-scripts` for kind `html` and `sandbox` for every other kind.
- [ ] Every POST route (`/api/bus/*` verbs and `/api/code/index`) refuses with 403 a request whose `Origin` header is present and is not the server's own origin, which covers the `Origin: null` a sandboxed frame sends, or whose `Sec-Fetch-Site` header is present and is not `same-origin`. Requests carrying neither header, such as CLI callers, and every GET route are unaffected.

- [ ] `bun test` and `go test ./...` are green; `make -C atomic bundle frontend` builds.


## Approach


A cookie-backed module store owned by the frontend replaces the `?member=` param at every site, the Plans aggregator gains a single-checkout fallback for non-git roots, and the top bar reads the on-screen checkout the slug view already resolves. See `docs/design/serve-realm-ux.md`.


## Change tree


```
atomic/internal/serve/
  plans.go                                        M  worktrees(): non-git root as one checkout; planBundle + Path, OutsideRoot
  plans_test.go                                   M  non-git root fixture; bundle path assertions
  api_plans.go                                    M  findPlansMember matches the member's prefix
  api_plans_test.go                               M  realm fixture whose root has no .git still yields rows; ?member= by prefix
  api_plans_page.go                               M  raw html responses send sandbox allow-scripts
  api_plans_page_test.go                          M  CSP per kind
  api_bus.go                                      M  POST verbs pass the cross-origin guard
  api_reindex.go                                  M  POST passes the cross-origin guard
  origin_guard.go                                 A  rejectCrossOrigin: Origin / Sec-Fetch-Site check for browser write requests
  origin_guard_test.go                            A
  frontend/src/
    components/plans/BundleFileViewer.tsx         M  iframe sandbox="allow-scripts"; header comment
    components/plans/BundleFileViewer.test.tsx    M
    utils/memberStore.ts                          A  identity fetch, cookie read/write, subscribe, useCurrentMember, memberLabel
    utils/memberStore.test.ts                     A
    utils/planViewStore.ts                        A  on-screen checkout published by SlugView, read by TopBar
    utils/planViewStore.test.ts                   A
    utils/plansApi.ts                             M  PlanBundle + path, outsideRoot
    components/plans/usePlansScope.ts             M  drop member; plansHref -> /plans; slugHref keeps at only
    components/plans/usePlansScope.test.tsx       M
    components/plans/PlansView.tsx                M  picker from store; realm-name label
    components/plans/PlansView.test.tsx           M
    components/plans/SlugView.tsx                 M  fetch gated on ready; publish on-screen checkout
    components/plans/SlugView.test.tsx            M
    components/plans/resolve.ts                   M  default newest by mtime
    components/plans/resolve.test.ts              M
    components/rail/PlansRail.tsx                 M  member from store, gated on ready
    components/rail/PlansRail.test.tsx            M
    components/search/SearchPalette.tsx           M  member from store
    components/search/SearchPalette.test.tsx      M
    components/nav/TopBar.tsx                     M  member crumb; provenance from planViewStore
    components/nav/TopBar.test.tsx                M
    pages/Graph/Graph.tsx                         M  member from store; fallback without write; realm-name label
    pages/Graph/Graph.test.tsx                    M
    components/schema/SchemaView.tsx              M  member from store; fallback without write
    components/schema/SchemaView.test.tsx         M
    components/schema/SchemaToolbar.tsx           M  picker writes the store; realm-name label
docs/design/serve-plans-page.md                   M  default-version flow; picker paragraph
docs/spec/serve-plans-page.md                     M  realm-root assumption; default-version criteria and flows
```


## Outline


```
atomic/internal/serve/plans.go
  worktrees — git enumerates; on git failure the root is the one checkout (checkoutID(root), branch "", isMain false)
  planBundle — Path, OutsideRoot from checkoutDisplayPath
  build — bundle entries carry their checkout's display path
frontend/src/utils/memberStore.ts
  state — identity { scope, name } | null, ready, member
  ensureIdentity — one in-flight GET /api/nav; on success read cookie entry, on failure identity null; ready true either way
  readCookie / writeCookie — atomic-member JSON map; encode/decode; try/catch around document
  setMember — set state; rewrite only this identity's entry
  useCurrentMember — hook returning { member, ready, scope, realmName, setMember }
  memberLabel — prefix, or realmName when prefix is ""
frontend/src/utils/planViewStore.ts
  state — onScreen { branch, path, outsideRoot } | null
  setOnScreen / clearOnScreen
  useOnScreenCheckout — hook
frontend/src/components/plans/usePlansScope.ts
  PlansScope — at, slug, relpath, isPlansRoute, openSlug, openFile, setAt, plansHref, slugHref
frontend/src/components/plans/resolve.ts
  newestByMtime — default when at does not resolve
frontend/src/components/nav/TopBar.tsx
  plansCrumbs — member crumb in realm scope; file crumb followed by provenance
  Provenance — branch · path
frontend/src/pages/Graph/Graph.tsx
  member — from store; resolveMember yields a render fallback only
frontend/src/components/schema/SchemaView.tsx
  member — from store; same fallback rule
frontend/src/components/schema/SchemaToolbar.tsx
  picker — label via memberLabel; onChange calls the store's setMember
atomic/internal/serve/origin_guard.go
  rejectCrossOrigin — 403 when Origin is present and not the request's own origin, or Sec-Fetch-Site is present and not same-origin; absent headers pass
atomic/internal/serve/api_plans_page.go
  raw branch — kind html sends sandbox allow-scripts; other kinds keep sandbox
frontend/src/components/plans/BundleFileViewer.tsx
  iframe — sandbox="allow-scripts"
```


## Flows


**Flow: first render in realm scope**

1. A consumer calls `useCurrentMember()`; the store starts `ensureIdentity()` once
2. The store GETs `/api/nav` and reads `scope` and `name`
3. The store decodes cookie `atomic-member` and looks up `<scope>:<name>`; a missing entry is `""`
4. The store flips `ready`; consumers that held their fetch now fetch with that member
5. Pickers render their member lists; the empty-prefix entry is labelled by `name`

**Flow: picking a repo**

1. The reader selects `server` in any picker
2. `setMember("api")` updates state and rewrites the cookie map entry for this identity
3. Every subscribed page refetches with `server`
4. A rail click to `/plans`, a reload, and a serve of the same realm on another port all land on `server`

**Flow: a page cannot honour the member**

1. Graph loads `/code/graph/members`; the stored member `website` is not among them
2. Graph renders its first member; the store and the cookie are untouched
3. Switching to Plans still shows `website`

**Flow: opening a plan file**

1. `SlugView` resolves the doc version or bundle for `?at=`; with no hold, the newest by mtime
2. `SlugView` publishes `{ branch, path, outsideRoot }` of the resolved checkout to `planViewStore`, and clears it on unmount
3. `TopBar` renders `plans › <member> › <slug> › <file>` and, after the file, `<branch> · <path>`

**Flow: a root that is not a git repository**

1. `git worktree list --porcelain` fails at the root
2. The aggregator uses the root as its one checkout: `checkoutID(root)`, branch `""`, `isMain` false, no `Created`
3. Each `docs/design` and `docs/spec` file yields one version with label `""`; each scratchpad bundle is attributed to that checkout with its display path
4. `/api/plans` returns the rows; Plans lists them under the realm-name entry

**Flow: a bundle mock runs**

1. The reader opens a bundle's `mock.html`; `BundleFileViewer` points an iframe with `sandbox="allow-scripts"` at the raw URL
2. The raw response for kind `html` carries `Content-Security-Policy: sandbox allow-scripts`; the document runs its inline scripts in an opaque origin
3. A script in the mock that POSTs to `/api/bus/say` sends `Origin: null`; `rejectCrossOrigin` answers 403 before the handler runs
4. The bus page's own POSTs carry the server's origin and pass; a CLI caller sends no `Origin` and passes


## Checkpoints


| # | Checkpoint | Files/areas | Agent | Est. files | Verifies |
|---|------------|-------------|-------|------------|----------|
| 1 | Non-git root as one checkout; bundle path in payload | `atomic/internal/serve/plans.go`, `plans_test.go`, `api_plans_test.go`, `frontend/src/utils/plansApi.ts` | atomic-implementer (mode: surgical) | ~2 (+tests) | `go test ./internal/serve/... -run Plans` green; a fixture root with no `.git` holding `docs/design/x.md`, `docs/spec/x.md`, and `.claude/.scratchpad/x/BRIEF.md` yields one row with two docs of one version each (label `""`, `isMain` false, checkout id `checkoutID(root)`, `Created` nil) and one bundle whose `Path` equals the checkout's display path; a stubbed `runGitWorktreeList` error takes the same path; every pre-existing Plans test passes; `PlanBundle` in `plansApi.ts` declares `path` and `outsideRoot` |
| 2 | Member store and the Plans consumers | `frontend/src/utils/memberStore.ts` (+test), `components/plans/usePlansScope.ts` (+test), `PlansView.tsx` (+test), `SlugView.tsx` (+test), `components/rail/PlansRail.tsx` (+test), `components/search/SearchPalette.tsx` (+test), `components/nav/TopBar.tsx` (member-free plans hrefs only) | atomic-implementer (mode: feature) | ~7 (+tests) | `bun test` green; store tests cover identity key `realm:acme`, cookie round trip, missing entry to `""`, `setMember` preserving other identities' entries, `ready` false until `/api/nav` resolves, failure leaving identity null and `ready` true; `grep -rn 'searchParams.get("member")' frontend/src/components/plans frontend/src/components/rail frontend/src/components/search frontend/src/components/nav` returns nothing; the Plans picker test renders `acme` for the empty prefix and `(local)` is absent from `components/plans`; `usePlansScope` tests assert `plansHref() === "/plans"`, `slugHref` carries only `at`, and `scopedSearch` no longer exists; Plans, PlansRail, SlugView, and SearchPalette hold their plan fetch until the store is ready |
| 3 | Graph and Schema consumers, realm-name label | `frontend/src/pages/Graph/Graph.tsx` (+test), `frontend/src/components/schema/SchemaView.tsx` (+test), `frontend/src/components/schema/SchemaToolbar.tsx` (owns the Schema picker and its label) | atomic-implementer (mode: feature) | ~3 (+tests) | `bun test` green; `grep -rn 'searchParams.get("member")' frontend/src --exclude='*.test.ts' --exclude='*.test.tsx'` returns nothing; `grep -rnE '[?&]member=' frontend/src --include='*.ts' --include='*.tsx' --exclude='*.test.ts' --exclude='*.test.tsx'` matches only server fetch URL builders (`utils/plansApi.ts`, `components/schema/SchemaView.tsx`'s schema fetch, `components/code-modal/types.ts`, `hooks/useReindex.ts`); Graph test: a stored member absent from Graph's member list renders the first member and the cookie is unchanged; Graph and Schema picker tests render `acme` for the empty prefix; `(local)` is absent from `frontend/src`; Graph and Schema hold their member-scoped fetch until the store is ready |
| 4 | Newest-by-mtime default | `frontend/src/components/plans/resolve.ts`, `resolve.test.ts` | atomic-implementer (mode: surgical) | ~1 (+test) | test: a doc whose merged version is older than a worktree version resolves to the worktree version when `at` is undefined, `held` false; a held `at` still wins; a yield (held name absent) also lands on newest; `PlansView` dot rendering (`dotMerged`) unchanged |
| 5 | Top-bar provenance | `frontend/src/utils/planViewStore.ts` (+test), `SlugView.tsx` (+test), `TopBar.tsx` (+test) | atomic-implementer (mode: feature) | ~3 (+tests) | TopBar tests: realm scope on `/plans/checkout-flow/docs/spec/checkout-flow.md` with store member `server` and published `{ branch: "main", path: "api" }` renders crumbs `plans › api › checkout-flow › …` and provenance `main · api`; a bundle file with branch `worktree-billing` and path `api/.claude/worktrees/billing` renders that pair; empty branch renders the path alone; repo scope omits the member crumb; navigating off the plans route clears the provenance; SlugView test asserts publish on resolve and clear on unmount |
| 6 | Bundle mocks run their own scripts; write routes refuse cross-origin browsers | `atomic/internal/serve/origin_guard.go` (+test), `api_plans_page.go` (+test), `api_bus.go`, `api_reindex.go`, `frontend/src/components/plans/BundleFileViewer.tsx` (+test) | atomic-implementer (mode: feature) | ~5 (+tests) | Go tests: a raw `.html` response carries `Content-Security-Policy: sandbox allow-scripts` and a raw `.md` or `.png` still carries `sandbox`; `POST /api/bus/say` with `Origin: null` returns 403 and never reaches the hub, with `Origin: http://<request host>` proceeds, and with no `Origin` proceeds; the same three cases for `POST /api/code/index`; `Sec-Fetch-Site: cross-site` alone is refused; every GET route is unaffected. Frontend test: the bundle `html` iframe's `sandbox` attribute equals `allow-scripts`. `bun test` and `go test ./internal/serve/...` green |


## Risks


| Risk | Likelihood | Mitigation |
|------|-----------|-----------|
| `/api/nav` fails on first load, so the store never learns its identity | low | `ready` still flips with identity null and member `""`; the cookie is neither read nor written; pickers still work from their own member lists for the session |
| Two serves of the same realm on different ports rewrite the cookie at once | low | Each `setMember` is a read-modify-write of one small map; last write wins; nothing else lives in it |
| happy-dom cookie semantics differ from a browser | low | Store tests assert through the store's own read after write; a manual check in a real browser against a running serve follows once every checkpoint is green |
| A bookmarked `?member=` URL | low | Unknown params are ignored; the page renders the stored member |
| A member that exists for Plans but not for Graph | medium | The fallback rule: Graph renders its first member and never writes the store, so the pick survives the detour |


## Change log


### 2026-09-02 — Initial spec

**What changed:** Spec written for the five serve fixes: non-git roots aggregate as one checkout, a cookie-backed frontend member store replaces `?member=`, the default version is newest by mtime, the realm root is labelled by the realm's name, and the top bar names the repo and checkout of an open plan file.

**Why:** Observed on a multi-repo realm: an empty realm entry over ten real docs, a repo pick that reset on every rail click, specs opening on `main` mid-work, and no on-screen provenance for a plan file. Design: `docs/design/serve-realm-ux.md`.

### 2026-09-02 — Correction: the Schema picker lives in SchemaToolbar

**What changed:** Checkpoint 3 names `components/schema/SchemaToolbar.tsx` alongside `SchemaView.tsx` and runs in feature mode; the change tree and outline carry the file.

**Why:** Correction: the surgical implementer found that `SchemaToolbar.tsx` renders the Schema member `<select>` and owns the `(local)` label; `SchemaView.tsx` holds only state and a dead `memberLabel`. A two-file cap could not satisfy the checkpoint's gates. The checkpoint's `searchParams.get("member")` gate now excludes test files, since a test's mock server legitimately parses that query from a fetch URL.

### 2026-09-02 — Correction: one identifier in the store, and the graph container survives the fallback

**What changed:** The Member store criteria name the stored value as the member's `prefix` and require `GET /api/plans?member=` to resolve by prefix (`api_plans.go` joins the change tree). A criterion requires the code graph's mount container to be attached to the document in the fallback case.

**Why:** Correction, from the delivery audit: Plans wrote the member `key` while Graph and Schema wrote the `prefix`; the two diverge for a declared member whose path has a directory component, so a pick in one page could 404 or fall back in another. Separately, Graph keyed its container on the resolved fallback member while its mount effect keyed on the stored member, so a fallback replaced the container after the mount began and the engine drew into a detached node.

### 2026-09-02 — Bundle mocks run their own scripts

**What changed:** A "Bundle mocks" criteria group, checkpoint 6, and matching change-tree, outline, and flow entries: `allow-scripts` on the bundle iframe and on the raw response's CSP for kind `html`, plus a cross-origin guard on every POST route. Two non-goals bound it: no scripts outside bundles, and no `allow-same-origin`, `allow-forms`, or `allow-popups`.

**Why:** Issue #234: interactive design mocks from `atomic-visual-options` rendered as a dead first frame because both the iframe and the raw response blocked scripts. Scripts can run only if the unauthenticated write routes stop trusting a browser request that is not same-origin, since a sandboxed frame shares the machine and passes the loopback check.


## Implementation log


### shipped — 2026-09-02

One squashed commit on `serve-realm-ux`, PR #235 against `next`. The work ran as five reviewed checkpoints (non-git root as one checkout and bundle path; member store and the Plans consumers; Graph and Schema consumers with the realm-name label; newest-by-mtime default; top-bar provenance), a docs pass over `docs/reference/serve.md`, one post-audit iteration, and a signals refresh.

Gates at ship: `go test ./...` no failures; `go vet` clean; `gofmt -l` empty; `bun test` 318 pass, 0 fail; `bunx tsc --noEmit` clean; `make -C atomic bundle frontend` ok; `npm run docs:build` ok; `atomic validate spec` exit 0; `atomic signals stale` exit 0. Live check with the dev binary against a realm whose root is a plain directory: the root returned its plan rows where it returned none before.

Audit: one iteration after `atomic-auditor` (graph container keyed on the fallback member; Plans stored `key` where Graph and Schema stored `prefix`; reset helper names; cookie unnamed in the reference; commit bodies unwrapped). No strategist dispatch. Follow-ups ledger empty.
