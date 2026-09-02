import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { typeIntoCombobox } from "../../test/typeIntoCombobox";
import { createMemoryRouter, MemoryRouter, RouterProvider } from "react-router";
import { ApiProvider } from "../../utils/api";
import { __resetForTest } from "../../utils/memberStore";
import { SearchPalette } from "./SearchPalette";
import type { ApiCodeSearchResponse, ApiMdSearchResponse } from "./types";
import type { PlanRow } from "../../utils/plansApi";

const NAV_FIXTURE = { scope: "realm", name: "acme", branch: "", groups: [] };

function seedMemberCookie(member: string) {
  document.cookie = `atomic-member=${encodeURIComponent(JSON.stringify({ "realm:acme": member }))}; path=/`;
}

// Every mount also triggers the member store's own GET /nav — call counts
// on the search/plans fetches are asserted net of that background request.
function nonNavCallCount(): number {
  const calls = (globalThis.fetch as unknown as { mock: { calls: [RequestInfo | URL][] } }).mock.calls;
  return calls.filter(([input]) => {
    const url = typeof input === "string" ? input : input.toString();
    return !url.includes("/nav");
  }).length;
}

const MD_FIXTURE: ApiMdSearchResponse = {
  query: "auth",
  truncated: false,
  cap: 50,
  results: [{ relpath: "wiki/auth.md", line: 3, snippet: "auth flow" }],
};

const CODE_FIXTURE: ApiCodeSearchResponse = {
  members: [
    { key: "repo", prefix: "repo", indexed: true, results: [{ id: "n1", name: "Authenticate", kind: "func", filePath: "auth.go", startLine: 10 }] },
    { key: "cold", prefix: "cold", indexed: false, results: [] },
  ],
};

const PLANS_FIXTURE: PlanRow[] = [
  { slug: "atomic-doctor", title: "atomic-doctor", description: "Verifies install and project-state coherence.", docs: [], bundles: [], dotCount: 0, dotMerged: false },
  { slug: "serve-plans-page", title: "Plans page", description: "Browse plans in the serve UI.", docs: [], bundles: [], dotCount: 0, dotMerged: false },
  { slug: "release-please-ci", title: "Release CI", description: "Fix broken release branches.", docs: [], bundles: [], dotCount: 0, dotMerged: false },
];

function mockFetchByUrl(handlers: Record<string, unknown>) {
  const withNav = { "/nav": NAV_FIXTURE, ...handlers };
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    for (const [match, body] of Object.entries(withNav)) {
      if (url.includes(match)) {
        return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
      }
    }
    return new Response(JSON.stringify({ error: "unexpected path" }), { status: 404 });
  }) as unknown as typeof fetch;
}

function renderPalette(open = true) {
  const onOpenChange = mock(() => {});
  render(
    <MemoryRouter>
      <ApiProvider>
        <SearchPalette open={open} onOpenChange={onOpenChange} />
      </ApiProvider>
    </MemoryRouter>,
  );
  return { onOpenChange };
}

describe("SearchPalette", () => {
  afterEach(() => {
    mock.restore();
    // @ts-expect-error — test-only global cleanup
    delete globalThis.fetch;
    __resetForTest();
    document.cookie = "atomic-member=; path=/; max-age=0";
  });

  test("⌘K opens the palette", async () => {
    mockFetchByUrl({});
    const { onOpenChange } = renderPalette(false);
    await userEvent.keyboard("{Meta>}k{/Meta}");
    expect(onOpenChange).toHaveBeenCalledWith(true);
  });

  test("Escape closes an open palette", async () => {
    mockFetchByUrl({});
    const { onOpenChange } = renderPalette(true);
    await userEvent.keyboard("{Escape}");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  test("'/' opens the palette when focus isn't in a text field", async () => {
    mockFetchByUrl({});
    const { onOpenChange } = renderPalette(false);
    await userEvent.keyboard("/");
    expect(onOpenChange).toHaveBeenCalledWith(true);
  });

  test("debounces typed input before fetching md results", async () => {
    mockFetchByUrl({ "/api/search/md": MD_FIXTURE });
    renderPalette(true);

    const input = screen.getByLabelText("Search");
    await typeIntoCombobox(input, "auth");

    // Immediately after typing, no search fetch should have happened yet.
    expect(nonNavCallCount()).toBe(0);

    await waitFor(() => expect(screen.getByText("wiki/auth.md")).toBeInTheDocument(), { timeout: 2000 });
    expect(nonNavCallCount()).toBe(1);
  });

  test("md|code toggle switches the fetch target", async () => {
    mockFetchByUrl({ "/api/search/md": MD_FIXTURE, "/api/code/search": CODE_FIXTURE });
    renderPalette(true);

    const input = screen.getByLabelText("Search");
    await typeIntoCombobox(input, "auth");
    await waitFor(() => expect(screen.getByText("wiki/auth.md")).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: "code" }));
    await waitFor(() => expect(screen.getByText("Authenticate")).toBeInTheDocument());
    expect(screen.getByText(/not indexed/)).toBeInTheDocument();
  });

  test("selecting a markdown result navigates and closes the palette", async () => {
    mockFetchByUrl({ "/api/search/md": MD_FIXTURE });
    const { onOpenChange } = renderPalette(true);

    const input = screen.getByLabelText("Search");
    await typeIntoCombobox(input, "auth");
    await waitFor(() => expect(screen.getByText("wiki/auth.md")).toBeInTheDocument());

    await userEvent.click(screen.getByText("wiki/auth.md"));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  test("plans tab fetches /api/plans once and filters the held payload by query", async () => {
    mockFetchByUrl({ "/api/plans": PLANS_FIXTURE });
    renderPalette(true);

    await userEvent.click(screen.getByRole("button", { name: "plans" }));
    await waitFor(() => expect(nonNavCallCount()).toBe(1));

    const input = screen.getByLabelText("Search");
    await typeIntoCombobox(input, "release");

    await waitFor(() => expect(screen.getByText("Release CI")).toBeInTheDocument());
    expect(screen.queryByText("atomic-doctor")).not.toBeInTheDocument();
    expect(screen.queryByText("Plans page")).not.toBeInTheDocument();
    // Filtering is client-side against the payload already held — no
    // second fetch fires as the query changes.
    expect(nonNavCallCount()).toBe(1);
  });

  test("selecting a plans result navigates to the slug and closes the palette", async () => {
    mockFetchByUrl({ "/api/plans": PLANS_FIXTURE });
    const { onOpenChange } = renderPalette(true);

    await userEvent.click(screen.getByRole("button", { name: "plans" }));
    const input = screen.getByLabelText("Search");
    await typeIntoCombobox(input, "release");
    await waitFor(() => expect(screen.getByText("Release CI")).toBeInTheDocument());

    await userEvent.click(screen.getByText("Release CI"));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  test("the stored member's plans fetch carries member=, navigate carries no search", async () => {
    seedMemberCookie("api");
    mockFetchByUrl({ "/api/plans": PLANS_FIXTURE });
    const router = createMemoryRouter(
      [
        { path: "/plans", element: <SearchPalette open={true} onOpenChange={() => {}} /> },
        { path: "/plans/:slug", element: <div>opened</div> },
      ],
      { initialEntries: ["/plans"] },
    );
    render(
      <ApiProvider>
        <RouterProvider router={router} />
      </ApiProvider>,
    );

    await userEvent.click(screen.getByRole("button", { name: "plans" }));
    const fetchMock = globalThis.fetch as unknown as { mock: { calls: [RequestInfo | URL][] } };
    await waitFor(() => {
      const plansCall = fetchMock.mock.calls.find(([input]) => {
        const url = typeof input === "string" ? input : input.toString();
        return url.includes("/plans") && !url.includes("/nav");
      });
      expect(plansCall).toBeDefined();
      const url = typeof plansCall![0] === "string" ? (plansCall![0] as string) : plansCall![0].toString();
      expect(url).toContain("member=api");
    });

    const input = screen.getByLabelText("Search");
    await typeIntoCombobox(input, "release");
    await waitFor(() => expect(screen.getByText("Release CI")).toBeInTheDocument());
    await userEvent.click(screen.getByText("Release CI"));

    await waitFor(() => expect(router.state.location.pathname).toBe("/plans/release-please-ci"));
    expect(router.state.location.search).toBe("");
  });
});
