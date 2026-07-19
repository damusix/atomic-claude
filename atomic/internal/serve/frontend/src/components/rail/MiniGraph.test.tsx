import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, waitFor } from "@testing-library/react";
import { __resetForTest, setNavigator } from "../../utils/graphUI";
import { __resetLoadScriptCacheForTest } from "../../utils/loadScript";
import { MiniGraph } from "./MiniGraph";

// A minimal stand-in for the carried Cytoscape global — MiniGraph only ever
// calls the handful of members exercised here (mount.ts's structural
// interface). Handlers registered via .on() are captured so tests can fire
// them directly, mirroring how Cytoscape would invoke them on a real
// mouseover/tap.
function stubCytoscape() {
  const handlers: Record<string, (evt: { target: unknown }) => void> = {};
  const node = {
    data: (key: string) =>
      ({ id: "wiki/other.md", type: "page", title: "Other", label: "other" })[key as never] ?? key,
    renderedPosition: () => ({ x: 5, y: 5 }),
  };
  const factory = mock(() => ({
    resize: () => {},
    fit: () => {},
    one: (_event: string, cb: () => void) => cb(),
    ready: (cb: () => void) => cb(),
    on: (event: string, _selector: string, cb: (evt: { target: unknown }) => void) => {
      handlers[event] = cb;
    },
  }));
  return { factory, handlers, node };
}

function mockGraphDataFetch(elements: unknown = []) {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify(elements), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      }),
  ) as unknown as typeof fetch;
}

// happy-dom's HTMLScriptElement throws synchronously on connect when real
// script-file loading is attempted (see loadScript.test.ts's stubScriptLoad
// comment) — substitute a <div> for "script" tags so loadScript() resolves
// without touching the network.
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

describe("MiniGraph", () => {
  afterEach(() => {
    mock.restore();
    __resetForTest();
    __resetLoadScriptCacheForTest();
    delete (window as { cytoscape?: unknown }).cytoscape;
    // registerRailCy (railCytoscapeStyle.ts) stashes the mounted Cytoscape
    // instance on this same module-level global — leaking it here makes
    // railCytoscapeStyle.test.ts's "nothing registered" case order-dependent.
    delete window.__railCy;
    document.body.innerHTML = "";
  });

  test("mounts the carried Cytoscape instance against the container with the focus node styled", async () => {
    const restoreAppend = stubScriptLoad();
    const { factory } = stubCytoscape();
    (window as unknown as { cytoscape: typeof factory }).cytoscape = factory;
    mockGraphDataFetch();

    render(<MiniGraph graphDataURL="/graph/data?node=wiki/index.md&depth=1" focusNode="wiki/index.md" />);

    await waitFor(() => expect(factory).toHaveBeenCalledTimes(1));
    const call = factory.mock.calls[0] as unknown as [{ container: HTMLElement }];
    expect(call[0].container).not.toBeNull();
    restoreAppend();
  });

  test("hover shows the preview card via utils/graphUI; click navigates via the registered navigator", async () => {
    const restoreAppend = stubScriptLoad();
    const { factory, handlers, node } = stubCytoscape();
    (window as unknown as { cytoscape: typeof factory }).cytoscape = factory;
    mockGraphDataFetch();

    const seen: string[] = [];
    setNavigator((id) => seen.push(id));

    render(<MiniGraph graphDataURL="/graph/data?node=wiki/index.md&depth=1" focusNode="wiki/index.md" />);
    await waitFor(() => expect(factory).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(handlers.mouseover).toBeDefined());

    handlers.mouseover({ target: node });
    const card = document.getElementById("cy-preview-card");
    expect(card?.classList.contains("open")).toBe(true);
    expect(card?.innerHTML).toContain("Other");

    handlers.tap({ target: node });
    expect(seen).toEqual(["wiki/other.md"]);
    restoreAppend();
  });
});
