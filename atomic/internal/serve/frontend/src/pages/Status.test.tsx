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

  test("realm scope: renders stale-item badges when not all fresh", async () => {
    mockFetchOnce({
      isRealmScope: true,
      wiki: {
        staleRepos: ["alpha"],
        staleConcerns: ["concern-x"],
        staleBuckets: ["research"],
        bucketDiffKeys: ["research"],
        allFresh: false,
      },
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
    expect(screen.getByText("concern-x")).toBeInTheDocument();
    expect(screen.getByText("research")).toBeInTheDocument();
    expect(screen.getByText("WARN")).toBeInTheDocument();
    expect(screen.getByText("foo")).toBeInTheDocument();
    expect(screen.getByText("baz")).toBeInTheDocument();
    expect(screen.queryByText("All fresh — realm is healthy.")).not.toBeInTheDocument();
  });

  test("realm scope, all fresh: renders the all-fresh summary and no stale badges", async () => {
    mockFetchOnce({
      isRealmScope: true,
      wiki: { staleRepos: [], staleConcerns: [], staleBuckets: [], bucketDiffKeys: [], allFresh: true },
      index: { severity: "PASS", detail: "code index: 5 fresh", freshCount: 5, staleMembers: [], notIndexed: [] },
    });

    renderStatus();

    await waitFor(() => expect(screen.getByText("All wiki artifacts are fresh.")).toBeInTheDocument());
    expect(screen.getByText("All fresh — realm is healthy.")).toBeInTheDocument();
  });

  test("repo scope: renders code-index health only, no wiki section", async () => {
    mockFetchOnce({
      isRealmScope: false,
      wiki: { staleRepos: [], staleConcerns: [], staleBuckets: [], bucketDiffKeys: [], allFresh: true },
      index: { severity: "PASS", detail: "code index: 1 fresh", freshCount: 1, staleMembers: [], notIndexed: [] },
    });

    renderStatus();

    await waitFor(() =>
      expect(screen.getByText("No realm wiki — showing repo code-index health only.")).toBeInTheDocument(),
    );
    expect(screen.queryByText("Wiki Staleness")).not.toBeInTheDocument();
  });
});
