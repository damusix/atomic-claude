import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { ApiProvider } from "../utils/api";
import { Status } from "./Status";

function mockFetchOnce(body: unknown, status = 200) {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
  ) as unknown as typeof fetch;
}

function renderStatus() {
  const router = createMemoryRouter([{ path: "/status", element: <Status /> }], {
    initialEntries: ["/status"],
  });
  return render(
    <ApiProvider>
      <RouterProvider router={router} />
    </ApiProvider>,
  );
}

describe("Status page", () => {
  afterEach(() => {
    mock.restore();
  });

  test("realm scope: lists each drifted member with a drift badge", async () => {
    mockFetchOnce({
      isRealmScope: true,
      wiki: { staleRepos: ["alpha", "libs/beta"], allFresh: false },
      index: {
        severity: "WARN",
        detail: "code index: 2 fresh; stale: foo (run atomic code sync); not indexed: baz",
        freshCount: 2,
        staleMembers: ["foo"],
        notIndexed: ["baz"],
      },
    });

    renderStatus();

    await waitFor(() => expect(screen.getByText("alpha")).toBeInTheDocument());
    expect(screen.getByText("libs/beta")).toBeInTheDocument();
    expect(screen.getByText("Wiki Membership Drift")).toBeInTheDocument();
    expect(screen.getAllByText("membership drift")).toHaveLength(2);
    expect(screen.getByText("WARN")).toBeInTheDocument();
    expect(screen.getByText("foo")).toBeInTheDocument();
    expect(screen.getByText("baz")).toBeInTheDocument();
    expect(screen.queryByText("All fresh — realm is healthy.")).not.toBeInTheDocument();
  });

  test("realm scope, no drift: renders the all-fresh summary and no drift badges", async () => {
    mockFetchOnce({
      isRealmScope: true,
      wiki: { staleRepos: [], allFresh: true },
      index: { severity: "PASS", detail: "code index: 5 fresh", freshCount: 5, staleMembers: [], notIndexed: [] },
    });

    renderStatus();

    await waitFor(() =>
      expect(screen.getByText("Wiki membership matches the last scan.")).toBeInTheDocument(),
    );
    expect(screen.queryByText("membership drift")).not.toBeInTheDocument();
    expect(screen.getByText("All fresh — realm is healthy.")).toBeInTheDocument();
  });

  test("repo scope: renders code-index health only, no wiki section", async () => {
    mockFetchOnce({
      isRealmScope: false,
      wiki: { staleRepos: [], allFresh: true },
      index: { severity: "PASS", detail: "code index: 1 fresh", freshCount: 1, staleMembers: [], notIndexed: [] },
    });

    renderStatus();

    await waitFor(() =>
      expect(screen.getByText("No realm wiki — showing repo code-index health only.")).toBeInTheDocument(),
    );
    expect(screen.queryByText("Wiki Membership Drift")).not.toBeInTheDocument();
  });
});
