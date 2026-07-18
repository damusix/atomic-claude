import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { ApiProvider } from "../utils/api";
import { External } from "./External";

function mockFetchOnce(body: unknown, status = 200) {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } }),
  ) as unknown as typeof fetch;
}

function renderExternal() {
  const router = createMemoryRouter([{ path: "/external", element: <External /> }], {
    initialEntries: ["/external"],
  });
  return render(
    <ApiProvider>
      <RouterProvider router={router} />
    </ApiProvider>,
  );
}

describe("External page", () => {
  afterEach(() => {
    mock.restore();
  });

  test("renders multi-source entries with sorted sources and first-seen date", async () => {
    mockFetchOnce({
      entries: [
        { url: "https://example.com/x", sources: ["pageA.md", "pageB.md"], firstSeen: "2024-05-01" },
      ],
    });

    renderExternal();

    await waitFor(() => expect(screen.getByText("https://example.com/x")).toBeInTheDocument());
    expect(screen.getByText("pageA.md")).toBeInTheDocument();
    expect(screen.getByText("pageB.md")).toBeInTheDocument();
    expect(screen.getByText("2024-05-01")).toBeInTheDocument();
  });

  test("renders an em-dash placeholder for a null firstSeen", async () => {
    mockFetchOnce({ entries: [{ url: "https://example.com/x", sources: ["a.md"], firstSeen: null }] });

    renderExternal();

    await waitFor(() => expect(screen.getByText("https://example.com/x")).toBeInTheDocument());
    expect(screen.getByText("—")).toBeInTheDocument();
  });

  test("renders the empty-registry message when there are no entries", async () => {
    mockFetchOnce({ entries: [] });

    renderExternal();

    await waitFor(() => expect(screen.getByText("No external links found in this realm.")).toBeInTheDocument());
  });
});
