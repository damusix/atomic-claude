import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { ApiProvider } from "./utils/api";
import { EventsProvider } from "./utils/events";
import { __resetGraphEngineLoadedForTest } from "./utils/graphEngineAdapter";
import { __resetLoadScriptCacheForTest } from "./utils/loadScript";
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

// Routes every fetch by URL so /api/nav, /api/page/*, and /api/rail/* each
// get a response shaped like their real handler — Page (CP6) now issues its
// own /api/page fetch on every route, no longer a text stub, so a single
// shared body (the pre-CP6 test setup) no longer works.
function mockNav(navBody: unknown = { scope: "repo", groups: [] }) {
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    if (url.includes("/api/nav")) {
      return new Response(JSON.stringify(navBody), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    if (url.includes("/api/page/")) {
      const relpath = decodeURIComponent(url.split("/api/page/")[1] ?? "");
      return new Response(
        JSON.stringify({
          html: `<p>Page: ${relpath}</p>`,
          title: relpath,
          relpath,
          hasMermaid: false,
          breadcrumb: [],
        }),
        { status: 200, headers: { "Content-Type": "application/json" } },
      );
    }
    // /api/rail/* and anything else: no rail for these routing-only tests.
    return new Response(JSON.stringify({ error: "not found" }), {
      status: 404,
      headers: { "Content-Type": "application/json" },
    });
  }) as unknown as typeof fetch;
}

// Stubs document.createElement("script") the same way graphEngineAdapter's
// own tests do — the /graph route lazy-loads the carried cosmos.gl vendor +
// engine + profile scripts on mount, and happy-dom throws on a real
// script-file fetch.
function stubScriptLoad() {
  const origCreate = document.createElement.bind(document);
  document.createElement = ((tag: string) => {
    if (tag === "script") {
      const el = origCreate("div") as unknown as HTMLScriptElement;
      queueMicrotask(() => el.dispatchEvent(new Event("load")));
      return el;
    }
    return origCreate(tag);
  }) as typeof document.createElement;
  return () => {
    document.createElement = origCreate;
  };
}

describe("App routing (Shell mount)", () => {
  afterEach(() => {
    mock.restore();
    __resetLoadScriptCacheForTest();
    __resetGraphEngineLoadedForTest();
    delete window.SystemGraph;
    delete window.CodeGraph;
  });

  test("the landing route mounts the Shell (top bar + nav pane + Page)", async () => {
    mockNav();
    renderAt("/");

    expect(document.getElementById("app-header")).not.toBeNull();
    expect(document.getElementById("nav-pane")).not.toBeNull();
    // The index route has no relpath — /api/page/ resolves the landing
    // server-side; the mock echoes the empty relpath back.
    await waitFor(() => expect(screen.getByText("Page:", { exact: false })).toBeInTheDocument());
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
    const restoreScripts = stubScriptLoad();
    window.SystemGraph = { mount: mock(() => {}), teardown: mock(() => {}), retheme: mock(() => {}) };
    window.CodeGraph = { mount: mock(() => {}), teardown: mock(() => {}), retheme: mock(() => {}) };
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
    restoreScripts();
  });
});
