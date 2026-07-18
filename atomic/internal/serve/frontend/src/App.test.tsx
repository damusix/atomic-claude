import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { ApiProvider } from "./utils/api";
import { EventsProvider } from "./utils/events";
import { routes } from "./routes";

function renderAt(path: string) {
  const router = createMemoryRouter(routes, { initialEntries: [path] });
  return render(
    <EventsProvider>
      <ApiProvider>
        <RouterProvider router={router} />
      </ApiProvider>
    </EventsProvider>,
  );
}

function mockNav(body: unknown = { scope: "repo", groups: [] }) {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
  ) as unknown as typeof fetch;
}

describe("App routing (Shell mount)", () => {
  afterEach(() => {
    mock.restore();
  });

  test("the landing route mounts the Shell (top bar + nav pane + page stub)", async () => {
    mockNav();
    renderAt("/");

    expect(document.getElementById("app-header")).not.toBeNull();
    expect(document.getElementById("nav-pane")).not.toBeNull();
    await waitFor(() => expect(screen.getByText(/\(landing\)/)).toBeInTheDocument());
  });

  test("/page/<relpath> resolves to the Page route with the relpath param", async () => {
    mockNav();
    renderAt("/page/wiki/index.md");

    await waitFor(() =>
      expect(screen.getByText("Page: wiki/index.md")).toBeInTheDocument(),
    );
  });

  test("/graph, /search, /status, /external each resolve to their own route stub", async () => {
    mockNav();
    for (const [path, marker] of [
      ["/graph", "graph"],
      ["/search", "search"],
      ["/status", "status"],
      ["/external", "external"],
    ] as const) {
      const { unmount } = renderAt(path);
      await waitFor(() =>
        expect(document.querySelector(`[data-route="${marker}"]`)).not.toBeNull(),
      );
      unmount();
    }
  });
});
