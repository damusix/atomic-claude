import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import type { PlanRow } from "../../utils/plansApi";
import { SlugView } from "./SlugView";

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
      purposes: ["implement"],
      status: "active",
      files: [{ relpath: "findings/volatility.md", kind: "markdown" }],
    },
  ],
  dotCount: 2,
  dotMerged: true,
};

function mockFetchByUrl(handlers: Record<string, unknown>) {
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    for (const [match, body] of Object.entries(handlers)) {
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

  test("?member= is carried into the /api/plans fetch", async () => {
    mockFetchByUrl({
      "/plans/page": { html: "<h1 id='goal'>Goal</h1><p>on main</p>", title: "spec", relpath: "docs/spec/atomic-doctor.md", hasMermaid: false, breadcrumb: [] },
      "/plans": [ROW],
    });
    renderSlug("/plans/atomic-doctor/docs/spec/atomic-doctor.md?member=server");

    await waitFor(() => expect(screen.getByText("on main")).toBeInTheDocument());

    const fetchMock = globalThis.fetch as unknown as { mock: { calls: [RequestInfo | URL][] } };
    const rowsCall = fetchMock.mock.calls.find(([input]) => {
      const url = typeof input === "string" ? input : input.toString();
      return url.includes("/plans") && !url.includes("/plans/page");
    });
    const calledUrl = typeof rowsCall![0] === "string" ? (rowsCall![0] as string) : rowsCall![0].toString();
    expect(calledUrl).toContain("member=server");
  });

  test("a slug absent from the scoped rows renders a one-line scope message, not a blank pane", async () => {
    mockFetchByUrl({ "/plans": [] });
    renderSlug("/plans/admin-sources?member=server");

    await waitFor(() => expect(screen.getByText(/No plan named/)).toBeInTheDocument());
    expect(screen.getByText("admin-sources", { selector: "code" })).toBeInTheDocument();
  });
});
