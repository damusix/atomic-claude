// Bus page rendering — the pure-function addressing tests live in
// Bus.test.tsx; this file covers the loopback-block notice, mirroring
// Page.test.tsx's fetch-mock setup/teardown.
import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { Bus } from "./Bus";

function renderAt(path: string) {
  const router = createMemoryRouter([{ path: "/bus", element: <Bus /> }], { initialEntries: [path] });
  return render(<RouterProvider router={router} />);
}

function mockFetchOnce(body: unknown, status = 200) {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
  ) as unknown as typeof fetch;
}

describe("Bus page", () => {
  afterEach(() => {
    mock.restore();
  });

  test("a 403 from /bus/status or /bus/rooms swaps the whole page for the loopback notice", async () => {
    mockFetchOnce({ error: "bus chat is loopback-only; connect from the serving machine" }, 403);

    renderAt("/bus");

    await waitFor(() => expect(screen.getByText("Message Bus is loopback-only")).toBeInTheDocument());
    expect(screen.queryByPlaceholderText("open a channel…")).not.toBeInTheDocument();
  });
});
