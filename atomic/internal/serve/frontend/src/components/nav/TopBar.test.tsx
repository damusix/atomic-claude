import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { act, render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, MemoryRouter, Outlet, RouterProvider } from "react-router";
import type { PlanRow } from "../../utils/plansApi";
import { __resetForTest as resetMemberStore } from "../../utils/memberStore";
import { __resetForTest as resetPlanViewStore, setOnScreen } from "../../utils/planViewStore";
import { SlugView } from "../plans/SlugView";
import { TopBar } from "./TopBar";

const DEFAULT_NAV = { scope: "repo", name: "atomic-claude", branch: "main", groups: [] };

function mockNav(body: unknown) {
  globalThis.fetch = mock(
    async () => new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } }),
  ) as unknown as typeof fetch;
}

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

const SIGNUP_FLOWS_ROW: PlanRow = {
  slug: "checkout-flow",
  title: "checkout-flow",
  description: "",
  docs: [
    {
      path: "docs/spec/checkout-flow.md",
      versions: [
        {
          sha: "sha-main",
          label: "main",
          isMain: true,
          mtime: "2026-08-19T00:00:00Z",
          checkouts: [
            { id: "w-main", branch: "main", path: ".", outsideRoot: false, isMain: true, fileMtime: "2026-08-19T00:00:00Z" },
          ],
        },
      ],
    },
  ],
  bundles: [],
  dotCount: 1,
  dotMerged: true,
};

function seedMemberCookie(identityKey: string, member: string) {
  document.cookie = `atomic-member=${encodeURIComponent(JSON.stringify({ [identityKey]: member }))}; path=/`;
}

describe("TopBar", () => {
  // ScopeChip and the member store both GET /nav on mount. Left unmocked, a
  // real fetch against no server hangs on ECONNRESET and the shared
  // FetchEngine's request dedupe can carry that stalled promise into a
  // later test that mocks the same URL — stub it every time by default.
  beforeEach(() => {
    mockNav(DEFAULT_NAV);
  });

  afterEach(() => {
    window.localStorage.clear();
    document.documentElement.removeAttribute("data-theme");
    mock.restore();
    // @ts-expect-error — test-only global cleanup
    delete globalThis.fetch;
    resetMemberStore();
    resetPlanViewStore();
    document.cookie = "atomic-member=; path=/; max-age=0";
  });

  test("renders a stubbed live-reload connection state", () => {
    render(
      <MemoryRouter>
        <TopBar connState="live" />
      </MemoryRouter>,
    );

    const indicator = document.querySelector(".conn-indicator");
    expect(indicator).toHaveAttribute("data-conn-state", "live");
    expect(screen.getByText("live")).toBeInTheDocument();
  });

  test("defaults to reconnecting when no connState is stubbed", () => {
    render(
      <MemoryRouter>
        <TopBar />
      </MemoryRouter>,
    );

    const indicator = document.querySelector(".conn-indicator");
    expect(indicator).toHaveAttribute("data-conn-state", "reconnecting");
  });

  test("renders the breadcrumb page label from the current route", () => {
    render(
      <MemoryRouter initialEntries={["/page/wiki/index.md"]}>
        <TopBar />
      </MemoryRouter>,
    );

    expect(document.querySelector(".breadcrumb-page")).toHaveTextContent("index.md");
  });

  test("renders every path segment, not just the leaf", () => {
    render(
      <MemoryRouter initialEntries={["/page/docs/wiki/serve.md"]}>
        <TopBar />
      </MemoryRouter>,
    );

    const crumbs = [...document.querySelectorAll(".breadcrumb-folder")].map((e) => e.textContent);
    expect(crumbs).toEqual(["docs", "wiki"]);
    expect(document.querySelector(".breadcrumb-page")).toHaveTextContent("serve.md");
  });

  test("plans slug route renders plans › slug › file, not a directory-listing split of the file path", () => {
    render(
      <MemoryRouter
        initialEntries={["/plans/agents-effort-config/docs/spec/agents-effort-config.md?at=main"]}
      >
        <TopBar />
      </MemoryRouter>,
    );

    const crumbs = [...document.querySelectorAll(".breadcrumb-folder")].map((e) => e.textContent);
    expect(crumbs).toEqual(["plans", "agents-effort-config"]);
    expect(document.querySelector(".breadcrumb-page")).toHaveTextContent("spec.md");
  });

  test("plans slug route links resolve to /plans and /plans/:slug, not /page/...", () => {
    render(
      <MemoryRouter initialEntries={["/plans/agents-effort-config/docs/spec/agents-effort-config.md?at=main"]}>
        <TopBar />
      </MemoryRouter>,
    );

    const links = [...document.querySelectorAll(".breadcrumb-folder")] as HTMLAnchorElement[];
    expect(links.map((l) => l.getAttribute("href"))).toEqual([
      "/plans",
      "/plans/agents-effort-config?at=main",
    ]);
  });

  test("bare /plans renders a single leaf crumb", () => {
    render(
      <MemoryRouter initialEntries={["/plans"]}>
        <TopBar />
      </MemoryRouter>,
    );

    expect(document.querySelectorAll(".breadcrumb-folder").length).toBe(0);
    expect(document.querySelector(".breadcrumb-page")).toHaveTextContent("plans");
  });

  // Modes and theme moved to components/nav/IconRail so a mode is switched in
  // exactly one place — the header must not grow a second control cluster.
  test("carries no view-mode or theme controls", () => {
    render(
      <MemoryRouter>
        <TopBar />
      </MemoryRouter>,
    );

    expect(document.getElementById("btn-graph")).toBeNull();
    expect(document.getElementById("btn-bus")).toBeNull();
    expect(document.querySelector(".theme-toggle")).toBeNull();
  });

  test("realm scope inserts a member crumb after plans and shows branch · path provenance", async () => {
    mockNav({ scope: "realm", name: "acme", branch: "", groups: [] });
    seedMemberCookie("realm:acme", "api");
    setOnScreen({ branch: "main", path: "api", outsideRoot: false });

    render(
      <MemoryRouter initialEntries={["/plans/checkout-flow/docs/spec/checkout-flow.md"]}>
        <TopBar />
      </MemoryRouter>,
    );

    await waitFor(() => {
      const crumbs = [...document.querySelectorAll(".breadcrumb-folder")].map((e) => e.textContent);
      expect(crumbs).toEqual(["plans", "api", "checkout-flow"]);
    });
    expect(document.querySelector(".breadcrumb-provenance")).toHaveTextContent("main · api");
    expect(screen.getByLabelText("Checkout: main · api")).toHaveClass("breadcrumb-provenance");
  });

  test("a bundle file's provenance names the bundle's own branch and path", async () => {
    mockNav({ scope: "realm", name: "acme", branch: "", groups: [] });
    seedMemberCookie("realm:acme", "api");
    setOnScreen({
      branch: "worktree-billing",
      path: "api/.claude/worktrees/billing",
      outsideRoot: false,
    });

    render(
      <MemoryRouter initialEntries={["/plans/billing/BRIEF.md"]}>
        <TopBar />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(document.querySelector(".breadcrumb-provenance")).toHaveTextContent(
        "worktree-billing · api/.claude/worktrees/billing",
      );
    });
  });

  test("an empty branch renders the path alone", async () => {
    mockNav({ scope: "repo", name: "api", branch: "", groups: [] });
    setOnScreen({ branch: "", path: ".", outsideRoot: false });

    render(
      <MemoryRouter initialEntries={["/plans/no-git/docs/spec/no-git.md"]}>
        <TopBar />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(document.querySelector(".breadcrumb-provenance")).toHaveTextContent(".");
    });
  });

  test("repo scope omits the member crumb", async () => {
    mockNav({ scope: "repo", name: "atomic-claude", branch: "main", groups: [] });
    setOnScreen({ branch: "main", path: ".", outsideRoot: false });

    render(
      <MemoryRouter initialEntries={["/plans/checkout-flow/docs/spec/checkout-flow.md"]}>
        <TopBar />
      </MemoryRouter>,
    );

    await waitFor(() => {
      const crumbs = [...document.querySelectorAll(".breadcrumb-folder")].map((e) => e.textContent);
      expect(crumbs).toEqual(["plans", "checkout-flow"]);
    });
  });

  test("a cleared on-screen checkout renders no provenance", async () => {
    mockNav({ scope: "realm", name: "acme", branch: "", groups: [] });

    render(
      <MemoryRouter initialEntries={["/plans/checkout-flow/docs/spec/checkout-flow.md"]}>
        <TopBar />
      </MemoryRouter>,
    );

    await waitFor(() => {
      expect(document.querySelectorAll(".breadcrumb-folder").length).toBeGreaterThan(0);
    });
    expect(document.querySelector(".breadcrumb-provenance")).toBeNull();
  });

  test("provenance clears on navigating from a slug file to bare /plans", async () => {
    mockFetchByUrl({
      "/nav": { scope: "repo", name: "atomic-claude", branch: "main", groups: [] },
      "/plans/page": { html: "<h1 id='goal'>Goal</h1><p>content</p>", title: "spec", relpath: "docs/spec/checkout-flow.md", hasMermaid: false, breadcrumb: [] },
      "/plans": [SIGNUP_FLOWS_ROW],
    });

    const router = createMemoryRouter(
      [
        {
          element: (
            <>
              <TopBar />
              <Outlet />
            </>
          ),
          children: [
            { path: "plans", element: <div /> },
            { path: "plans/:slug/*", element: <SlugView /> },
          ],
        },
      ],
      { initialEntries: ["/plans/checkout-flow/docs/spec/checkout-flow.md?at=main"] },
    );
    render(<RouterProvider router={router} />);

    await waitFor(() => {
      expect(document.querySelector(".breadcrumb-provenance")).toHaveTextContent("main · .");
    });

    await act(async () => {
      await router.navigate("/plans");
    });

    await waitFor(() => {
      expect(document.querySelector(".breadcrumb-provenance")).toBeNull();
    });

    // isPlansRoute stays true on bare /plans (it matches "/plans" and every
    // "/plans/..." descendant alike), so gating on it — instead of on
    // scope.slug, which is undefined here — would show provenance again the
    // instant anything repopulates the store while still on this route.
    act(() => {
      setOnScreen({ branch: "main", path: ".", outsideRoot: false });
    });
    expect(document.querySelector(".breadcrumb-provenance")).toBeNull();
  });
});
