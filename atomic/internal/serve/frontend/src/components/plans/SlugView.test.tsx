import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import type { PlanRow } from "../../utils/plansApi";
import { __resetForTest as resetMemberStore } from "../../utils/memberStore";
import { __resetForTest as resetPlanViewStore, getOnScreen } from "../../utils/planViewStore";
import { SlugView } from "./SlugView";

const NAV_FIXTURE = { scope: "realm", name: "acme", branch: "", groups: [] };

function seedMemberCookie(member: string) {
  document.cookie = `atomic-member=${encodeURIComponent(JSON.stringify({ "realm:acme": member }))}; path=/`;
}

function checkout(overrides: Partial<PlanRow["docs"][number]["versions"][number]["checkouts"][number]> = {}) {
  return {
    id: "w-main",
    branch: "main",
    path: ".",
    outsideRoot: false,
    isMain: true,
    fileMtime: "2026-08-19T00:00:00Z",
    ...overrides,
  };
}

// One doc (spec.md) with two versions: `main` (merged) and
// `scope-marker-docs`. A bundle exists ONLY on `plans-page`, holding
// findings/volatility.md — the file the yield test opens.
const ROW: PlanRow = {
  slug: "atomic-doctor",
  title: "atomic-doctor",
  description: "Verifies install and project-state coherence.",
  docs: [
    {
      path: "docs/spec/atomic-doctor.md",
      versions: [
        {
          sha: "sha-main",
          label: "main",
          isMain: true,
          mtime: "2026-08-19T00:00:00Z",
          // plans-page hasn't diverged this doc from main, so it shares
          // main's version — the aggregator's own way of surfacing every
          // worktree id, which is how the yield resolves a branch name for
          // a bundle-only worktree.
          checkouts: [checkout({ id: "w-main", branch: "main" }), checkout({ id: "w-plans-page", branch: "plans-page", path: ".claude/worktrees/plans-page", isMain: false })],
        },
        {
          sha: "sha-scope",
          label: "scope-marker-docs",
          isMain: false,
          mtime: "2026-08-18T00:00:00Z",
          checkouts: [
            checkout({ id: "w-scope", branch: "scope-marker-docs", path: ".claude/worktrees/scope-marker-docs", isMain: false }),
          ],
        },
      ],
    },
  ],
  bundles: [
    {
      worktreeId: "w-plans-page",
      branch: "plans-page",
      path: ".claude/worktrees/plans-page",
      outsideRoot: false,
      purposes: ["implement"],
      status: "active",
      files: [{ relpath: "findings/volatility.md", kind: "markdown" }],
    },
  ],
  dotCount: 2,
  dotMerged: true,
};

// Bundle-only worktree: `w-standalone` never appears in any doc version's
// checkouts (mid-implementation, no docs/design|spec written yet), so
// findCheckoutById(row, "w-standalone") returns undefined.
const ROW_BUNDLE_ONLY_WORKTREE: PlanRow = {
  slug: "bundle-only",
  title: "bundle-only",
  description: "",
  docs: [
    {
      path: "docs/spec/bundle-only.md",
      versions: [
        {
          sha: "sha-main",
          label: "main",
          isMain: true,
          mtime: "2026-08-19T00:00:00Z",
          checkouts: [checkout({ id: "w-main", branch: "main" })],
        },
      ],
    },
  ],
  bundles: [
    {
      worktreeId: "w-standalone",
      branch: "standalone-wt",
      path: ".claude/worktrees/standalone-wt",
      outsideRoot: false,
      purposes: ["implement"],
      status: "active",
      files: [{ relpath: ".claude/worktrees/standalone-wt/.claude/.scratchpad/bundle-only/BRIEF.md", kind: "markdown" }],
    },
  ],
  dotCount: 1,
  dotMerged: true,
};

function mockFetchByUrl(handlers: Record<string, unknown>) {
  const withNav = { "/nav": NAV_FIXTURE, ...handlers };
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    for (const [match, body] of Object.entries(withNav)) {
      if (url.includes(match)) {
        return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
      }
    }
    return new Response(JSON.stringify({ error: "unexpected path: " + url }), { status: 404 });
  }) as unknown as typeof fetch;
}

function renderSlug(initialPath: string) {
  const router = createMemoryRouter(
    [{ path: "/plans/:slug/*", element: <SlugView /> }],
    { initialEntries: [initialPath] },
  );
  return { router, ...render(<RouterProvider router={router} />) };
}

describe("SlugView", () => {
  afterEach(() => {
    mock.restore();
    // @ts-expect-error — test-only global cleanup
    delete globalThis.fetch;
    resetMemberStore();
    resetPlanViewStore();
    document.cookie = "atomic-member=; path=/; max-age=0";
  });

  test("a file the selected checkout holds renders that version with the selection unchanged", async () => {
    mockFetchByUrl({
      "/plans/page": { html: "<h1 id='goal'>Goal</h1><p>on scope-marker-docs</p>", title: "spec", relpath: "docs/spec/atomic-doctor.md", hasMermaid: false, breadcrumb: [] },
      "/plans": [ROW],
    });
    const { router } = renderSlug("/plans/atomic-doctor/docs/spec/atomic-doctor.md?at=scope-marker-docs");

    await waitFor(() => expect(screen.getByText("on scope-marker-docs")).toBeInTheDocument());
    expect(router.state.location.search).toBe("?at=scope-marker-docs");

    const fetchMock = globalThis.fetch as unknown as { mock: { calls: [RequestInfo | URL][] } };
    const pageCall = fetchMock.mock.calls.find(([input]) => (typeof input === "string" ? input : input.toString()).includes("/plans/page"));
    expect(pageCall).toBeDefined();
    const calledUrl = typeof pageCall![0] === "string" ? (pageCall![0] as string) : pageCall![0].toString();
    expect(calledUrl).toContain("worktree=w-scope");
  });

  test("opening a bundle file present in only one checkout renders it and moves the selection to that checkout", async () => {
    mockFetchByUrl({
      "/plans/page": { html: "<h1 id='f1'>F1</h1><p>the finding</p>", title: "volatility", relpath: "findings/volatility.md", hasMermaid: false, breadcrumb: [] },
      "/plans": [ROW],
    });
    const { router } = renderSlug("/plans/atomic-doctor/findings/volatility.md?at=scope-marker-docs");

    await waitFor(() => expect(screen.getByText("the finding")).toBeInTheDocument());
    await waitFor(() => expect(router.state.location.search).toBe("?at=plans-page"));

    const fetchMock = globalThis.fetch as unknown as { mock: { calls: [RequestInfo | URL][] } };
    const pageCall = fetchMock.mock.calls.find(([input]) => (typeof input === "string" ? input : input.toString()).includes("/plans/page"));
    const calledUrl = typeof pageCall![0] === "string" ? (pageCall![0] as string) : pageCall![0].toString();
    expect(calledUrl).toContain("worktree=w-plans-page");
  });

  test("a committed doc held by the selection renders with ?at= unchanged (inverse of the yield)", async () => {
    mockFetchByUrl({
      "/plans/page": { html: "<h1 id='goal'>Goal</h1><p>on main</p>", title: "spec", relpath: "docs/spec/atomic-doctor.md", hasMermaid: false, breadcrumb: [] },
      "/plans": [ROW],
    });
    const { router } = renderSlug("/plans/atomic-doctor/docs/spec/atomic-doctor.md?at=main");

    await waitFor(() => expect(screen.getByText("on main")).toBeInTheDocument());
    expect(router.state.location.search).toBe("?at=main");
  });

  test("the store's member is carried into the /api/plans fetch", async () => {
    seedMemberCookie("api");
    mockFetchByUrl({
      "/plans/page": { html: "<h1 id='goal'>Goal</h1><p>on main</p>", title: "spec", relpath: "docs/spec/atomic-doctor.md", hasMermaid: false, breadcrumb: [] },
      "/plans": [ROW],
    });
    renderSlug("/plans/atomic-doctor/docs/spec/atomic-doctor.md");

    await waitFor(() => expect(screen.getByText("on main")).toBeInTheDocument());

    const fetchMock = globalThis.fetch as unknown as { mock: { calls: [RequestInfo | URL][] } };
    const rowsCall = fetchMock.mock.calls.find(([input]) => {
      const url = typeof input === "string" ? input : input.toString();
      return url.includes("/plans") && !url.includes("/plans/page");
    });
    const calledUrl = typeof rowsCall![0] === "string" ? (rowsCall![0] as string) : rowsCall![0].toString();
    expect(calledUrl).toContain("member=api");
  });

  test("the plan fetch holds until the member store is ready", async () => {
    mockFetchByUrl({
      "/plans/page": { html: "<h1 id='goal'>Goal</h1><p>on main</p>", title: "spec", relpath: "docs/spec/atomic-doctor.md", hasMermaid: false, breadcrumb: [] },
      "/plans": [ROW],
    });
    renderSlug("/plans/atomic-doctor/docs/spec/atomic-doctor.md");

    expect(screen.getByText("Loading…")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("on main")).toBeInTheDocument());
  });

  test("a bundle file in a worktree holding no docs fetches by the bundle's own worktree id", async () => {
    mockFetchByUrl({
      "/plans/page": { html: "<h1 id='b'>B</h1><p>standalone content</p>", title: "BRIEF", relpath: ".claude/worktrees/standalone-wt/.claude/.scratchpad/bundle-only/BRIEF.md", hasMermaid: false, breadcrumb: [] },
      "/plans": [ROW_BUNDLE_ONLY_WORKTREE],
    });
    renderSlug(
      "/plans/bundle-only/.claude/worktrees/standalone-wt/.claude/.scratchpad/bundle-only/BRIEF.md?at=standalone-wt",
    );

    await waitFor(() => expect(screen.getByText("standalone content")).toBeInTheDocument());

    const fetchMock = globalThis.fetch as unknown as { mock: { calls: [RequestInfo | URL][] } };
    const pageCall = fetchMock.mock.calls.find(([input]) => (typeof input === "string" ? input : input.toString()).includes("/plans/page"));
    expect(pageCall).toBeDefined();
    const calledUrl = typeof pageCall![0] === "string" ? (pageCall![0] as string) : pageCall![0].toString();
    expect(calledUrl).toContain("worktree=w-standalone");
  });

  test("a slug absent from the scoped rows renders a one-line scope message, not a blank pane", async () => {
    seedMemberCookie("api");
    mockFetchByUrl({ "/plans": [] });
    renderSlug("/plans/admin-sources");

    await waitFor(() => expect(screen.getByText(/No plan named/)).toBeInTheDocument());
    expect(screen.getByText("admin-sources", { selector: "code" })).toBeInTheDocument();
  });

  test("publishes the resolved doc's checkout to planViewStore, and clears it on unmount", async () => {
    mockFetchByUrl({
      "/plans/page": { html: "<h1 id='goal'>Goal</h1><p>on main</p>", title: "spec", relpath: "docs/spec/atomic-doctor.md", hasMermaid: false, breadcrumb: [] },
      "/plans": [ROW],
    });
    const { unmount } = renderSlug("/plans/atomic-doctor/docs/spec/atomic-doctor.md?at=main");

    await waitFor(() => expect(screen.getByText("on main")).toBeInTheDocument());
    expect(getOnScreen()).toEqual({ branch: "main", path: ".", outsideRoot: false });

    unmount();

    expect(getOnScreen()).toBeNull();
  });

  test("publishes a bundle file's checkout, not the doc it yielded from", async () => {
    mockFetchByUrl({
      "/plans/page": { html: "<h1 id='f1'>F1</h1><p>the finding</p>", title: "volatility", relpath: "findings/volatility.md", hasMermaid: false, breadcrumb: [] },
      "/plans": [ROW],
    });
    renderSlug("/plans/atomic-doctor/findings/volatility.md?at=scope-marker-docs");

    await waitFor(() => expect(screen.getByText("the finding")).toBeInTheDocument());

    expect(getOnScreen()).toEqual({
      branch: "plans-page",
      path: ".claude/worktrees/plans-page",
      outsideRoot: false,
    });
  });
});
