import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { MemoryRouter } from "react-router";
import type { PlanRow } from "../../utils/plansApi";
import { __resetForTest } from "../../utils/memberStore";
import { PlansRail } from "./PlansRail";

const NAV_FIXTURE = { scope: "realm", name: "acme", branch: "", groups: [] };

function seedMemberCookie(member: string) {
  document.cookie = `atomic-member=${encodeURIComponent(JSON.stringify({ "realm:acme": member }))}; path=/`;
}

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
          checkouts: [{ id: "w-main", branch: "main", path: ".", outsideRoot: false, isMain: true, fileMtime: "2026-08-19T00:00:00Z" }],
        },
      ],
    },
  ],
  bundles: [],
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

function renderRail(initialPath: string) {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <PlansRail />
    </MemoryRouter>,
  );
}

describe("PlansRail", () => {
  afterEach(() => {
    mock.restore();
    // @ts-expect-error — test-only global cleanup
    delete globalThis.fetch;
    __resetForTest();
    document.cookie = "atomic-member=; path=/; max-age=0";
  });

  test("carries the store's member into its own /api/plans fetch", async () => {
    seedMemberCookie("api");
    mockFetchByUrl({ "/plans": [ROW] });
    renderRail("/plans/atomic-doctor/docs/spec/atomic-doctor.md");

    await waitFor(() => expect(screen.getByText("spec.md")).toBeInTheDocument());

    const fetchMock = globalThis.fetch as unknown as { mock: { calls: [RequestInfo | URL][] } };
    const plansCall = fetchMock.mock.calls.find(([input]) => {
      const url = typeof input === "string" ? input : input.toString();
      return url.includes("/plans") && !url.includes("/nav");
    });
    const url = typeof plansCall![0] === "string" ? (plansCall![0] as string) : plansCall![0].toString();
    expect(url).toContain("member=api");
  });

  test("holds its own fetch until the member store is ready", async () => {
    mockFetchByUrl({ "/plans": [ROW] });
    renderRail("/plans/atomic-doctor/docs/spec/atomic-doctor.md");

    await waitFor(() => expect(screen.getByText("spec.md")).toBeInTheDocument());
  });

  test("renders no Links or Graph tabs", async () => {
    mockFetchByUrl({ "/plans": [ROW] });
    renderRail("/plans/atomic-doctor/docs/spec/atomic-doctor.md");

    await waitFor(() => expect(screen.getByText("spec.md")).toBeInTheDocument());

    expect(screen.queryByText("Links")).not.toBeInTheDocument();
    expect(screen.queryByText("Graph")).not.toBeInTheDocument();
    expect(screen.queryByText("Bundle", { selector: "button, [role=tab]" })).not.toBeInTheDocument();
  });

  // Two worktrees both mid-plan on the same slug produce two bundles that
  // both hold BRIEF.md at the same worktree-relative relpath — the rail must
  // render both, headed by branch, with only the ?at= selected one carrying
  // the "on" highlight.
  const BUNDLE_RELPATH = ".claude/.scratchpad/atomic-doctor/BRIEF.md";
  const TWO_BUNDLE_ROW: PlanRow = {
    ...ROW,
    bundles: [
      { worktreeId: "w-main", branch: "main", path: ".", outsideRoot: false, purposes: ["plan"], status: "active", files: [{ relpath: BUNDLE_RELPATH, kind: "markdown" }] },
      { worktreeId: "w-feature", branch: "feature-x", path: ".claude/worktrees/feature-x", outsideRoot: false, purposes: ["implement"], status: "active", files: [{ relpath: BUNDLE_RELPATH, kind: "markdown" }] },
    ],
  };

  test("two bundles holding the same relpath render two groups with branch headers; only the ?at= match is highlighted", async () => {
    mockFetchByUrl({ "/plans": [TWO_BUNDLE_ROW] });
    renderRail(`/plans/atomic-doctor/${BUNDLE_RELPATH}?at=feature-x`);

    await waitFor(() => expect(screen.getByText("main")).toBeInTheDocument());
    expect(screen.getByText("feature-x", { selector: ".bnav-group-header" })).toBeInTheDocument();
    // Fixed by F-3: the picked bundle's own branch now drives the static
    // Version display, no longer suppressed by an unresolved checkout.
    expect(screen.getByText("feature-x", { selector: ".vpick-static" })).toBeInTheDocument();

    const entries = screen.getAllByText("BRIEF.md");
    expect(entries).toHaveLength(2);
    const onEntries = entries.filter((el) => el.className.includes("on"));
    expect(onEntries).toHaveLength(1);
  });

  test("clicking a bundle entry opens it at its own checkout's branch", async () => {
    mockFetchByUrl({ "/plans": [TWO_BUNDLE_ROW] });
    const router = createMemoryRouter(
      [{ path: "/plans/:slug/*", element: <PlansRail /> }],
      { initialEntries: [`/plans/atomic-doctor/${BUNDLE_RELPATH}`] },
    );
    render(<RouterProvider router={router} />);

    await waitFor(() => expect(screen.getByText("feature-x")).toBeInTheDocument());
    const entries = screen.getAllByText("BRIEF.md");
    await userEvent.click(entries[1]);

    await waitFor(() => expect(router.state.location.search).toBe("?at=feature-x"));
    expect(router.state.location.pathname).toBe(`/plans/atomic-doctor/${BUNDLE_RELPATH}`);
  });
});
