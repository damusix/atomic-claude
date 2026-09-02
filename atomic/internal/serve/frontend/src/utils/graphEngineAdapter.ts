// graphEngineAdapter — mount/teardown glue for the carried cosmos.gl engine
// (public/graph-core.js + its two profiles, public/system-graph.js and
// public/code-graph.js). Never rebuilds engine logic: this module only lazy-
// loads the carried vendor/profile scripts (in dependency order — graph-core
// must execute before either profile, since both read window.GraphCore at
// their own top-level IIFE init), delegates mount/teardown to the profiles'
// own exported window.SystemGraph / window.CodeGraph objects, and reimplements
// the member-picker resolution logic that system-graph.js's mountCodeView()
// used to run as htmx-shell orchestration (not part of the carried engine —
// it directly manipulated #main-pane's innerHTML, which has no React
// equivalent, so GraphRoute owns this behavior instead; see
// docs/spec/serve-react-frontend.md's "Flow: graph-mode mount" and
// frontend/CLAUDE.md's carried-code section).
//
// Colors are never derived here — the carried profiles read them from the
// typeColors window global (installed in CP5) exactly as they did pre-React;
// this module has no palette logic of its own.
import { attempt } from "@logosdx/utils";
import { api } from "./api";
import { loadScript } from "./loadScript";
import { memberLabel } from "./memberStore";

export type GraphView = "docs" | "code";

export interface GraphMember {
  prefix: string;
  indexed: boolean;
}

interface GraphMembersResponse {
  members: GraphMember[];
}

interface GraphProfileHandle {
  mount(container: HTMLElement, member?: string): void;
  teardown(): void;
  retheme(): void;
}

declare global {
  interface Window {
    // The carried engine core (graph-core.js) — SystemGraph/CodeGraph's own
    // mount() delegates here with their respective profile object. Declared
    // for test doubles that reproduce that delegation; production code in
    // this module never calls it directly (no access to either profile).
    GraphCore?: {
      mount(container: HTMLElement, profile: unknown): void;
      teardown(): void;
      retheme(): void;
    };
    SystemGraph?: Pick<GraphProfileHandle, "mount" | "teardown" | "retheme">;
    CodeGraph?: GraphProfileHandle;
  }
}

let engineLoaded: Promise<void> | null = null;

// ensureGraphEngineLoaded lazy-loads the carried vendor + engine + profile
// scripts at most once per session, in the order graph-core.js's own header
// comment requires (vendored cosmos.gl bundle, then graph-core.js, then the
// profiles — see that file's comment for why order matters).
export function ensureGraphEngineLoaded(): Promise<void> {
  if (!engineLoaded) {
    engineLoaded = loadScript("/vendor/cosmos-graph.js")
      .then(() => loadScript("/graph-core.js"))
      .then(() => Promise.all([loadScript("/system-graph.js"), loadScript("/code-graph.js")]))
      .then(() => undefined);
  }
  return engineLoaded;
}

// fetchGraphMembers reads the carried, non-/api/* /code/graph/members
// endpoint (untouched path per the API contracts conventions) — resolved to
// an absolute URL so the one shared FetchEngine instance (fixed to baseUrl
// /api) bypasses its baseUrl for this request, same pattern as
// components/rail/MiniGraph's graphDataURL fetch.
export async function fetchGraphMembers(): Promise<GraphMember[]> {
  const url = new URL("/code/graph/members", window.location.origin).toString();
  const [res, err] = await attempt(() => api.get<GraphMembersResponse>(url));
  if (err || !res?.ok || !res.data) return [];
  return res.data.members ?? [];
}

// resolveMember ports system-graph.js's mountCodeView() member-resolution
// rule: a realm with more than one member and no meaningful "empty member"
// fallback picks the first discovered member when the requested one is
// unrecognized; single-member scope (<=1 member) leaves the request
// unchanged (member stays '' either way — the local index).
export function resolveMember(members: GraphMember[], requested: string): string {
  if (members.length > 1 && !members.some((m) => m.prefix === requested)) {
    return members[0]?.prefix ?? "";
  }
  return requested;
}

export function pickerLabel(m: GraphMember, realmName: string): string {
  return memberLabel(m.prefix, realmName) + (m.indexed ? "" : " — not indexed");
}

// mountGraph loads the engine (idempotent) then delegates to the matching
// carried profile's own mount() — which internally calls
// window.GraphCore.mount(container, profile). The profile's hover/click
// hooks resolve through window.AtomicGraphUI (utils/graphUI, installed by
// Shell) and, for the code view, window.AtomicCodeExplorer (CP9 — the
// carried code guards its absence with `if (window.AtomicCodeExplorer)`, so
// no stub is required here).
export async function mountGraph(container: HTMLElement, view: GraphView, member?: string): Promise<void> {
  await ensureGraphEngineLoaded();
  if (view === "code") {
    window.CodeGraph?.mount(container, member);
  } else {
    window.SystemGraph?.mount(container);
  }
}

// Test-only reset — clears the module-level "engine already loaded" cache so
// each test file (bun:test shares one module registry across files, unlike
// Jest's per-file isolation) starts from a cold ensureGraphEngineLoaded().
export function __resetGraphEngineLoadedForTest(): void {
  engineLoaded = null;
}

// rethemeGraph re-pushes point/link colors on the live carried engine
// instance (window.GraphCore.retheme(), part of the theme toggle retheme
// cascade — CP11). A no-op with no mounted graph, per graph-core.js's own
// retheme() guard (instance may be null between navigations).
export function rethemeGraph(): void {
  window.GraphCore?.retheme();
}

// teardownGraph releases the WebGL context via the matching profile's
// teardown() (both profiles delegate to the same window.GraphCore.teardown()
// under the hood, so either call is equivalent — the view argument just picks
// which profile object is guaranteed to exist without having loaded the
// other).
export function teardownGraph(view: GraphView): void {
  if (view === "code") {
    window.CodeGraph?.teardown();
  } else {
    window.SystemGraph?.teardown();
  }
}
