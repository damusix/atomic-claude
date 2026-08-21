import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, MemoryRouter, RouterProvider } from "react-router";
import type { PlanRow } from "../../utils/plansApi";
import { PlansView } from "./PlansView";

function checkout(id: string, branch: string): PlanRow["docs"][number]["versions"][number]["checkouts"][number] {
  return {
    id,
    branch,
    path: `.claude/worktrees/${branch}`,
    outsideRoot: false,
    isMain: branch === "main",
    fileMtime: "2026-08-19T00:00:00Z",
  };
}

const NO_DESC_ROW: PlanRow = {
  slug: "no-desc-plan",
  title: "No Description Plan",
  description: "",
  docs: [
    {
      path: "docs/spec/no-desc-plan.md",
      versions: [{ sha: "a1", label: "main", isMain: true, mtime: "2026-08-19T00:00:00Z", checkouts: [checkout("w1", "main")] }],
    },
  ],
  bundles: [],
  dotCount: 1,
  dotMerged: true,
};

const DESC_ROW: PlanRow = {
  slug: "serve-plans-page",
  title: "Scratchpad bundles and the Plans surface",
  description: "One directory per slug, aggregated across every worktree.",
  docs: [
    { path: "docs/design/serve-plans-page.md", versions: [] },
    {
      path: "docs/spec/serve-plans-page.md",
      versions: [
        { sha: "a1", label: "main", isMain: true, mtime: "2026-08-19T00:00:00Z", checkouts: [checkout("w1", "main")] },
        { sha: "b2", label: "plans-page", isMain: false, mtime: "2026-08-20T00:00:00Z", checkouts: [checkout("w2", "plans-page")] },
        { sha: "c3", label: "wip", isMain: false, mtime: "2026-08-18T00:00:00Z", checkouts: [checkout("w3", "wip")] },
        { sha: "d4", label: "other", isMain: false, mtime: "2026-08-17T00:00:00Z", checkouts: [checkout("w4", "other")] },
      ],
    },
  ],
  bundles: [
    {
      worktreeId: "w2",
      purposes: ["plan"],
      status: "active",
      files: [
        { relpath: "BRIEF.md", kind: "markdown" },
        { relpath: "STATE.md", kind: "markdown" },
        { relpath: "findings/lens-1.md", kind: "markdown" },
      ],
    },
  ],
  dotCount: 4,
  dotMerged: true,
};

const FIVE_WORKTREE_ROW: PlanRow = {
  ...NO_DESC_ROW,
  slug: "five-worktree-plan",
  docs: [
    {
      path: "docs/spec/five-worktree-plan.md",
      versions: [1, 2, 3, 4, 5].map((n) => ({
        sha: `sha${n}`,
        label: `branch-${n}`,
        isMain: n === 1,
        mtime: "2026-08-19T00:00:00Z",
        checkouts: [checkout(`wt${n}`, `branch-${n}`)],
      })),
    },
  ],
  dotCount: 5,
  dotMerged: true,
};

// Go serializes a nil slice as JSON `null`, not `[]` — a row for a slug
// whose bundle was never populated (e.g. a bundle-less doc-only slug) ships
// bundles/docs as null, exactly like the real /api/plans response does.
const NULL_ARRAYS_ROW = {
  slug: "docs-only-plan",
  title: "Docs Only Plan",
  description: "",
  docs: null,
  bundles: null,
  dotCount: 0,
  dotMerged: false,
} as unknown as PlanRow;

function mockFetchByUrl(handlers: Record<string, unknown>) {
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    for (const [match, body] of Object.entries(handlers)) {
      if (url.includes(match)) {
        return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
      }
    }
    return new Response(JSON.stringify({ error: "unexpected path" }), { status: 404 });
  }) as unknown as typeof fetch;
}

function renderPlans(pathname = "/plans") {
  return render(
    <MemoryRouter initialEntries={[pathname]}>
      <PlansView />
    </MemoryRouter>,
  );
}

describe("PlansView", () => {
  afterEach(() => {
    mock.restore();
    // @ts-expect-error — test-only global cleanup
    delete globalThis.fetch;
  });

  test("no description collapses the row to one line", async () => {
    mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [NO_DESC_ROW] });
    renderPlans();

    await waitFor(() => expect(screen.getByText("No Description Plan")).toBeInTheDocument());
    expect(screen.queryByText(/\S/, { selector: ".plans-row-desc" })).not.toBeInTheDocument();
  });

  test("a row shows when it was last touched, from updatedAt", async () => {
    const row: PlanRow = { ...NO_DESC_ROW, updatedAt: "2026-08-14T10:00:00Z" };
    mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [row] });
    renderPlans();

    await waitFor(() => expect(screen.getByText("No Description Plan")).toBeInTheDocument());
    const line = document.querySelector(".plans-row-updated");
    expect(line).not.toBeNull();
    expect(line!.textContent).toMatch(/^updated \S/);
  });

  test("a zero-time updatedAt renders no updated line rather than year 0001", async () => {
    const row: PlanRow = { ...NO_DESC_ROW, updatedAt: "0001-01-01T00:00:00Z" };
    mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [row] });
    renderPlans();

    await waitFor(() => expect(screen.getByText("No Description Plan")).toBeInTheDocument());
    expect(document.querySelector(".plans-row-updated")).toBeNull();
  });

  test("a multi-version row renders one dot per version, with the merged one filled", async () => {
    mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [DESC_ROW] });
    renderPlans();

    await waitFor(() => expect(screen.getByText(DESC_ROW.title)).toBeInTheDocument());
    const dots = document.querySelectorAll(".plans-dot");
    expect(dots.length).toBe(4);
    const filled = document.querySelectorAll(".plans-dot[data-filled]");
    expect(filled.length).toBe(1);
  });

  test("a bundle row renders exactly the chips its files justify", async () => {
    mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [DESC_ROW] });
    renderPlans();

    await waitFor(() => expect(screen.getByText(DESC_ROW.title)).toBeInTheDocument());
    expect(screen.getByText("design")).toBeInTheDocument();
    expect(screen.getByText("spec")).toBeInTheDocument();
    expect(screen.getByText("brief")).toBeInTheDocument();
    expect(screen.getByText("state")).toBeInTheDocument();
    expect(screen.getByText("findings")).toBeInTheDocument();
    expect(screen.queryByText("followups")).not.toBeInTheDocument();
    expect(screen.queryByText("options")).not.toBeInTheDocument();
  });

  test("no checkout control renders for a single-worktree fixture", async () => {
    mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [NO_DESC_ROW] });
    renderPlans();

    await waitFor(() => expect(screen.getByText("No Description Plan")).toBeInTheDocument());
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  test("no checkout control renders for a five-worktree fixture", async () => {
    mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [FIVE_WORKTREE_ROW] });
    renderPlans();

    await waitFor(() => expect(screen.getByText("five-worktree-plan")).toBeInTheDocument());
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
  });

  test("repo scope (one member) renders no member picker", async () => {
    mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [NO_DESC_ROW] });
    renderPlans();

    await waitFor(() => expect(screen.getByText("No Description Plan")).toBeInTheDocument());
    expect(screen.queryByLabelText("Repo")).not.toBeInTheDocument();
  });

  test("a multi-member fixture renders a memberSelect that drives fetchPlans(member)", async () => {
    mockFetchByUrl({
      "/plans/members": {
        members: [
          { key: "", prefix: "" },
          { key: "atomic", prefix: "atomic" },
          { key: "taxgentic", prefix: "taxgentic" },
        ],
      },
      "/plans": [NO_DESC_ROW],
    });
    renderPlans();

    const select = await screen.findByLabelText("Repo");
    expect(select).toBeInTheDocument();

    const fetchMock = globalThis.fetch as unknown as { mock: { calls: [RequestInfo | URL][] }; mockClear: () => void };
    fetchMock.mockClear();
    await userEvent.selectOptions(select, "atomic");

    await waitFor(() => {
      const calledWithMember = fetchMock.mock.calls.some(([input]) => {
        const url = typeof input === "string" ? input : input.toString();
        return url.includes("/plans?member=atomic");
      });
      expect(calledWithMember).toBe(true);
    });
  });

  test("a row whose docs/bundles are null (Go's nil-slice JSON) renders without crashing", async () => {
    mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [NULL_ARRAYS_ROW] });
    renderPlans();

    await waitFor(() => expect(screen.getByText("Docs Only Plan")).toBeInTheDocument());
    expect(screen.getByText("Docs Only Plan").closest(".plans-row")?.querySelector(".plans-row-chips")).toBeNull();
  });

  test("row click navigates to /plans/<slug>", async () => {
    mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [NO_DESC_ROW] });
    const router = createMemoryRouter(
      [
        { path: "/plans", element: <PlansView /> },
        { path: "/plans/:slug", element: <div>opened: {"{slug}"}</div> },
      ],
      { initialEntries: ["/plans"] },
    );
    render(<RouterProvider router={router} />);

    const row = await screen.findByText("No Description Plan");
    await userEvent.click(row);

    await waitFor(() => expect(router.state.location.pathname).toBe("/plans/no-desc-plan"));
  });

  test("row click carries ?member= into the slug route", async () => {
    mockFetchByUrl({
      "/plans/members": { members: [{ key: "", prefix: "" }, { key: "server", prefix: "server" }] },
      "/plans": [NO_DESC_ROW],
    });
    const router = createMemoryRouter(
      [
        { path: "/plans", element: <PlansView /> },
        { path: "/plans/:slug", element: <div>opened: {"{slug}"}</div> },
      ],
      { initialEntries: ["/plans?member=server"] },
    );
    render(<RouterProvider router={router} />);

    const row = await screen.findByText("No Description Plan");
    await userEvent.click(row);

    await waitFor(() => expect(router.state.location.pathname).toBe("/plans/no-desc-plan"));
    expect(router.state.location.search).toBe("?member=server");
  });

  describe("filter", () => {
    test("typing narrows rows and updates the count", async () => {
      mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [NO_DESC_ROW, DESC_ROW] });
      renderPlans();

      await waitFor(() => expect(screen.getByText("No Description Plan")).toBeInTheDocument());
      const input = screen.getByPlaceholderText("filter plans");
      await userEvent.type(input, DESC_ROW.slug);

      await waitFor(() => {
        expect(screen.getByText(DESC_ROW.title)).toBeInTheDocument();
        expect(screen.queryByText("No Description Plan")).not.toBeInTheDocument();
      });
      expect(screen.getByText("1 of 2")).toBeInTheDocument();
    });

    test("an empty match renders a message naming the query, not a blank pane", async () => {
      mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [NO_DESC_ROW] });
      renderPlans();

      await waitFor(() => expect(screen.getByText("No Description Plan")).toBeInTheDocument());
      const input = screen.getByPlaceholderText("filter plans");
      await userEvent.type(input, "no-such-plan-anywhere");

      await waitFor(() => {
        expect(screen.getByText('No plans match "no-such-plan-anywhere".')).toBeInTheDocument();
      });
    });

    test("⌘F on /plans focuses the filter input and prevents the browser default", async () => {
      mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [NO_DESC_ROW] });
      renderPlans();

      await waitFor(() => expect(screen.getByText("No Description Plan")).toBeInTheDocument());
      const input = screen.getByPlaceholderText("filter plans") as HTMLInputElement;
      expect(document.activeElement).not.toBe(input);

      const event = new KeyboardEvent("keydown", { key: "f", metaKey: true, cancelable: true, bubbles: true });
      window.dispatchEvent(event);

      expect(document.activeElement).toBe(input);
      expect(event.defaultPrevented).toBe(true);
    });

    test("⌘F while an already-focused text field does nothing", async () => {
      mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [NO_DESC_ROW] });
      renderPlans();
      await waitFor(() => expect(screen.getByText("No Description Plan")).toBeInTheDocument());

      const outside = document.createElement("input");
      document.body.appendChild(outside);
      outside.focus();
      expect(document.activeElement).toBe(outside);

      const event = new KeyboardEvent("keydown", { key: "f", metaKey: true, cancelable: true, bubbles: true });
      window.dispatchEvent(event);

      expect(document.activeElement).toBe(outside);
      expect(event.defaultPrevented).toBe(false);
      outside.remove();
    });

    test("Esc in the filter input clears it and blurs", async () => {
      mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [NO_DESC_ROW, DESC_ROW] });
      renderPlans();
      await waitFor(() => expect(screen.getByText("No Description Plan")).toBeInTheDocument());

      const input = screen.getByPlaceholderText("filter plans") as HTMLInputElement;
      await userEvent.type(input, DESC_ROW.slug);
      expect(input.value).toBe(DESC_ROW.slug);

      await userEvent.keyboard("{Escape}");

      await waitFor(() => expect(input.value).toBe(""));
      expect(document.activeElement).not.toBe(input);
    });

    test("unmounting removes the ⌘F listener", async () => {
      mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [NO_DESC_ROW] });
      const { unmount } = renderPlans();
      await waitFor(() => expect(screen.getByText("No Description Plan")).toBeInTheDocument());
      unmount();

      const event = new KeyboardEvent("keydown", { key: "f", metaKey: true, cancelable: true, bubbles: true });
      window.dispatchEvent(event);

      expect(event.defaultPrevented).toBe(false);
    });

    test("filtering never reorders rows — the API's order is preserved", async () => {
      mockFetchByUrl({ "/plans/members": { members: [{ key: "", prefix: "" }] }, "/plans": [DESC_ROW, NO_DESC_ROW] });
      renderPlans();
      await waitFor(() => expect(screen.getByText(DESC_ROW.title)).toBeInTheDocument());

      const titles = () => Array.from(document.querySelectorAll(".plans-row-title")).map((el) => el.textContent);
      expect(titles()).toEqual([DESC_ROW.title, "No Description Plan"]);

      const input = screen.getByPlaceholderText("filter plans");
      await userEvent.type(input, "plan");

      await waitFor(() => expect(titles()).toEqual([DESC_ROW.title, "No Description Plan"]));
    });
  });
});
