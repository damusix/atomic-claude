import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { ApiProvider } from "../../utils/api";
import { events } from "../../utils/events";
import { __resetLoadScriptCacheForTest } from "../../utils/loadScript";
import { Rail } from "./Rail";
import type { RailResponse } from "./types";

// Every RAIL_FIXTURE below carries a graphDataURL, so every test in this file
// mounts MiniGraph, which lazy-loads the carried Cytoscape vendor script via
// loadScript(). Left unstubbed, that hits happy-dom's real script-element
// connect path (see loadScript.test.ts's stubScriptLoad comment) — locally
// that rejects synchronously, but it's a browser-API implementation detail,
// not a contract: on CI it was observed to not settle promptly, riding
// "clears the rail"'s awaits to bun's 5s per-test timeout. Stubbing the
// script element (and providing a minimal window.cytoscape so MiniGraph's
// mount promise chain always resolves the same way) decouples this file's
// assertions from that environment-dependent timing entirely.
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

function stubCytoscape() {
  return mock(() => ({
    resize: () => {},
    fit: () => {},
    one: (_event: string, cb: () => void) => cb(),
    ready: (cb: () => void) => cb(),
    on: () => {},
    style: () => {},
  }));
}

const RAIL_FIXTURE: RailResponse = {
  relpath: "wiki/index.md",
  orphan: false,
  properties: [
    { key: "type", value: "Repo", isURL: false, isJSON: false },
    { key: "resource", value: "https://example.com", isURL: true, isJSON: false },
    { key: "tags", value: '["a","b"]', isURL: false, isJSON: true },
  ],
  out: [
    {
      target: "notes.md",
      resolvedPath: "notes.md",
      broken: false,
      ambiguous: false,
      codeFile: false,
      external: false,
    },
    {
      target: "gone.md",
      resolvedPath: "",
      broken: true,
      ambiguous: false,
      codeFile: false,
      external: false,
    },
    {
      target: "https://example.com",
      resolvedPath: "",
      broken: false,
      ambiguous: false,
      codeFile: false,
      external: true,
    },
    {
      target: "render.go",
      resolvedPath: "atomic/internal/serve/render.go",
      broken: false,
      ambiguous: false,
      codeFile: true,
      external: false,
    },
  ],
  in: [{ path: "other.md" }],
  graphDataURL: "/graph/data?node=wiki/index.md&depth=1",
};

function mockFetchOnce(body: unknown, status = 200) {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
  ) as unknown as typeof fetch;
}

// Counts only /api/rail/* calls out of a fetch mock — MiniGraph's own
// graphDataURL fetch shares the same globalThis.fetch and would otherwise
// inflate a spy meant to track Rail's own refetch-on-realm.changed behavior.
function railCallCount(fetchSpy: ReturnType<typeof mock>): number {
  return fetchSpy.mock.calls.filter(([input]: [RequestInfo | URL]) =>
    (typeof input === "string" ? input : input.toString()).includes("/rail/"),
  ).length;
}

describe("Rail", () => {
  let restoreScriptLoad: () => void;

  beforeEach(() => {
    restoreScriptLoad = stubScriptLoad();
    (window as unknown as { cytoscape: ReturnType<typeof stubCytoscape> }).cytoscape = stubCytoscape();
  });

  afterEach(() => {
    mock.restore();
    // Module-scope cache — reset so no test's script-load resolution leaks
    // into a later file's own loadScript assertions.
    __resetLoadScriptCacheForTest();
    restoreScriptLoad();
    delete (window as { cytoscape?: unknown }).cytoscape;
  });

  test("renders nothing but the bare aside until page.resolved fires", () => {
    render(
      <ApiProvider>
        <Rail />
      </ApiProvider>,
    );
    const aside = document.getElementById("right-rail");
    expect(aside).not.toBeNull();
    expect(aside?.querySelector("#rail-props")).toBeNull();
  });

  test("fetches /api/rail/<relpath> on page.resolved and renders Properties/OUT/IN panels", async () => {
    mockFetchOnce(RAIL_FIXTURE);
    render(
      <ApiProvider>
        <Rail />
      </ApiProvider>,
    );

    events.emit("page.resolved", { relpath: "wiki/index.md" });

    await waitFor(() => expect(document.querySelector("#rail-props")).not.toBeNull());

    // Properties: plain, isURL anchor, isJSON pretty-printed block.
    const props = within(document.getElementById("rail-props-content") as HTMLElement);
    expect(props.getByText("type")).toBeInTheDocument();
    expect(props.getByText("Repo")).toBeInTheDocument();
    const urlAnchor = props.getByText("https://example.com") as HTMLAnchorElement;
    expect(urlAnchor.tagName).toBe("A");
    expect(urlAnchor.target).toBe("_blank");
    expect(document.querySelector(".rail-prop-json")).not.toBeNull();

    // OUT: resolved link, broken span, external new-tab, codeFile /file/ link.
    const out = within(document.getElementById("rail-out-content") as HTMLElement);
    expect(out.getByText("notes.md").closest("a")).toHaveAttribute("href", "/page/notes.md");
    expect(out.getByText("gone.md")).toHaveClass("wikilink-broken");
    expect(out.getByText("render.go").closest("a")).toHaveAttribute(
      "href",
      "/file/atomic/internal/serve/render.go",
    );

    // IN backlinks.
    const inLinks = within(document.getElementById("rail-in-content") as HTMLElement);
    expect(inLinks.getByText("other.md").closest("a")).toHaveAttribute("href", "/page/other.md");
  });

  test("clears the rail (hides panels) when page.resolved carries a null relpath", async () => {
    mockFetchOnce(RAIL_FIXTURE);
    render(
      <ApiProvider>
        <Rail />
      </ApiProvider>,
    );

    events.emit("page.resolved", { relpath: "wiki/index.md" });
    await waitFor(() => expect(document.querySelector("#rail-props")).not.toBeNull());

    events.emit("page.resolved", { relpath: null });
    await waitFor(() => expect(document.querySelector("#rail-props")).toBeNull());
  });

  test("a page with no graph membership (404 from /api/rail) shows the bare aside, not an error", async () => {
    mockFetchOnce({ error: "not found" }, 404);
    render(
      <ApiProvider>
        <Rail />
      </ApiProvider>,
    );

    events.emit("page.resolved", { relpath: "orphan.md" });

    // Give the 404 a beat to land, then assert it stays that way — a
    // fixed-tick setTimeout races the attempt()/FetchEngine promise chain.
    await new Promise((r) => setTimeout(r, 20));
    await waitFor(() => expect(document.querySelector("#rail-props")).toBeNull());
  });

  test("refetches /api/rail/<relpath> on realm.changed when the open relpath is in the changed list", async () => {
    const fetchSpy = mock(
      async () =>
        new Response(JSON.stringify(RAIL_FIXTURE), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    globalThis.fetch = fetchSpy as unknown as typeof fetch;

    render(
      <ApiProvider>
        <Rail />
      </ApiProvider>,
    );

    events.emit("page.resolved", { relpath: "wiki/index.md" });
    await waitFor(() => expect(document.querySelector("#rail-props")).not.toBeNull());
    expect(railCallCount(fetchSpy)).toBe(1);

    await act(async () => {
      events.emit("realm.changed", { fp: "fp2", changed: ["wiki/index.md"] });
    });

    await waitFor(() => expect(railCallCount(fetchSpy)).toBe(2));
  });

  test("does not refetch on realm.changed when the open relpath is absent from a bounded changed list", async () => {
    const fetchSpy = mock(
      async () =>
        new Response(JSON.stringify(RAIL_FIXTURE), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    globalThis.fetch = fetchSpy as unknown as typeof fetch;

    render(
      <ApiProvider>
        <Rail />
      </ApiProvider>,
    );

    events.emit("page.resolved", { relpath: "wiki/index.md" });
    await waitFor(() => expect(document.querySelector("#rail-props")).not.toBeNull());
    expect(railCallCount(fetchSpy)).toBe(1);

    await act(async () => {
      events.emit("realm.changed", { fp: "fp2", changed: ["some/other.md"] });
    });

    await new Promise((r) => setTimeout(r, 20));
    expect(railCallCount(fetchSpy)).toBe(1);
  });
});
