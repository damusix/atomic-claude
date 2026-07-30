import { afterAll, describe, expect, mock, test } from "bun:test";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import { ApiProvider } from "../../utils/api";
import { events } from "../../utils/events";

// Rail mounts MiniGraph whenever a fixture carries graphDataURL. The graph
// subsystem itself (Cytoscape lazy-load, layout wiring, hover/click) is
// covered in isolation by MiniGraph.test.tsx — this file only asserts the
// Properties/OUT/IN panels and the clear-on-null behavior, so mocking
// MiniGraph out here removes an entire async surface these tests never
// needed exercised (script load + a FetchEngine graph-data request that
// MiniGraph never passes an AbortController for, so it's still in flight
// when a test unmounts it) with zero coverage loss.
//
// mock.module(id, factory) mutates the SAME exports object require(id)
// already returned, in place — it does not swap in a fresh one. So capturing
// `const RealMiniGraph = require("./MiniGraph")` and reading
// `RealMiniGraph.MiniGraph` later (e.g. from an afterAll trying to "restore"
// the real module) silently reads the now-stubbed function: RealMiniGraph
// and the mocked module are the same object (verified directly). This is
// what corrupted every attempt to un-mock MiniGraph for a later file (e.g.
// MiniGraph.test.tsx on Linux CI) — restoring never worked because there was
// nothing real left to restore. Destructuring the function out immediately
// detaches a real reference from that object's future mutation; a plain
// module-level flag (read at render time, so it's live for every future
// import of "./MiniGraph" too — in this file or any later one) then chooses
// between the stub and the real component.
const { MiniGraph: RealMiniGraphComponent }: typeof import("./MiniGraph") = require("./MiniGraph");
let useMiniGraphStub = true;

mock.module("./MiniGraph", () => ({
  // Rendered through JSX (a distinct element, its own fiber/hooks) rather
  // than calling RealMiniGraphComponent(props) as a plain function — the
  // latter would run its hooks against the wrapper's own fiber instead.
  MiniGraph: (props: Parameters<typeof RealMiniGraphComponent>[0]) =>
    useMiniGraphStub ? null : <RealMiniGraphComponent {...props} />,
}));

import { Rail } from "./Rail";
import type { RailResponse } from "./types";

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

describe("Rail", () => {
  // Deliberately no per-test mock.restore(): every test reassigns
  // globalThis.fetch fresh, so there's no cross-test mock-call-history to
  // clean up. Flips the shared flag once, for the whole file, in afterAll —
  // the wrapper registered above starts delegating to the real component for
  // every subsequent render, in this file or any later one.
  afterAll(() => {
    useMiniGraphStub = false;
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

    await act(async () => {
      events.emit("page.resolved", { relpath: "wiki/index.md" });
    });

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

    await act(async () => {
      events.emit("page.resolved", { relpath: "wiki/index.md" });
    });
    await waitFor(() => expect(document.querySelector("#rail-props")).not.toBeNull());

    await act(async () => {
      events.emit("page.resolved", { relpath: null });
    });
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
    expect(fetchSpy).toHaveBeenCalledTimes(1);

    await act(async () => {
      events.emit("realm.changed", { fp: "fp2", changed: ["wiki/index.md"] });
    });

    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(2));
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
    expect(fetchSpy).toHaveBeenCalledTimes(1);

    await act(async () => {
      events.emit("realm.changed", { fp: "fp2", changed: ["some/other.md"] });
    });

    await new Promise((r) => setTimeout(r, 20));
    expect(fetchSpy).toHaveBeenCalledTimes(1);
  });
});
