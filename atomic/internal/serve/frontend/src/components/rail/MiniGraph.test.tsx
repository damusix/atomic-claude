import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
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
  // Selector-less (background) handlers, kept apart from element handlers.
  const bgHandlers: Record<string, (evt: { target: unknown }) => void> = {};
  const destroy = mock(() => {});
  // classes records add/removeClass so the label-reveal on hover is observable
  // — the style rule it drives lives in cytoscape, which is stubbed here.
  const classes = new Set<string>();
  const node = {
    data: (key: string) =>
      ({ id: "wiki/other.md", type: "page", title: "Other", label: "other" })[key as never] ?? key,
    renderedPosition: () => ({ x: 5, y: 5 }),
    addClass: (name: string) => classes.add(name),
    removeClass: (name: string) => classes.delete(name),
  };
  const factory = mock(() => ({
    resize: () => {},
    fit: () => {},
    destroy,
    one: (_event: string, cb: () => void) => cb(),
    ready: (cb: () => void) => cb(),
    // Both arities: cy.on(event, selector, cb) for element events and
    // cy.on(event, cb) for the background. Collapsing them would let the
    // background handler overwrite the element one under the same key.
    on: (
      event: string,
      selectorOrCb: string | ((evt: { target: unknown }) => void),
      maybeCb?: (evt: { target: unknown }) => void,
    ) => {
      if (typeof selectorOrCb === "function") {
        bgHandlers[event] = selectorOrCb;
        return;
      }
      if (maybeCb) handlers[event] = maybeCb;
    },
  }));
  return { factory, handlers, bgHandlers, node, destroy, classes };
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
  // afterEach only clears document.body — a real (unstubbed) script tag left
  // in document.head by an earlier file would otherwise survive into this
  // file's first case, since loadScript()'s "existing" branch matches on the
  // live DOM, not the reset load cache. Defensive; the actual CI hang here
  // (Rail.test.tsx's mock.module() corrupting its own captured "real module"
  // reference in place, so the component this file imported was still the
  // stub) is fixed at its source in Rail.test.tsx.
  beforeEach(() => {
    __resetForTest();
    __resetLoadScriptCacheForTest();
    delete (window as { cytoscape?: unknown }).cytoscape;
    delete window.__railCy;
    document.head.innerHTML = "";
  });

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

  test("hover only reveals the label; click opens the card, whose Open button navigates", async () => {
    const restoreAppend = stubScriptLoad();
    const { factory, handlers, node, classes } = stubCytoscape();
    (window as unknown as { cytoscape: typeof factory }).cytoscape = factory;
    mockGraphDataFetch();

    const seen: string[] = [];
    setNavigator((id) => seen.push(id));

    render(<MiniGraph graphDataURL="/graph/data?node=wiki/index.md&depth=1" focusNode="wiki/index.md" />);
    await waitFor(() => expect(factory).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(handlers.mouseover).toBeDefined());

    // Hover marks the node so its label draws, and does NOT raise the card —
    // two things under one pointer covered each other.
    handlers.mouseover({ target: node });
    expect(classes.has("hovered")).toBe(true);
    // The card element is created on first show, so "not open" here means
    // absent OR present-and-closed.
    const openNow = () =>
      document.getElementById("cy-preview-card")?.classList.contains("open") ?? false;
    expect(openNow()).toBe(false);

    handlers.mouseout({ target: node });
    expect(classes.has("hovered")).toBe(false);

    // Click raises the card instead of navigating away.
    handlers.tap({ target: node });
    expect(openNow()).toBe(true);
    const card = document.getElementById("cy-preview-card");
    expect(card?.innerHTML).toContain("Other");
    expect(seen).toEqual([]);

    // Navigation is the button's job now, so reading a node cannot cost you
    // the page you were on.
    const open = card?.querySelector<HTMLButtonElement>(".cy-pc-open");
    expect(open).not.toBeNull();
    open?.click();
    expect(seen).toEqual(["wiki/other.md"]);
    restoreAppend();
  });

  // The rail mounts this component per Graph-tab selection, so an instance
  // that outlives its unmount leaks a canvas and its listeners on every
  // switch — not once per session.
  test("destroys the Cytoscape instance when unmounted", async () => {
    const restoreAppend = stubScriptLoad();
    const { factory, destroy } = stubCytoscape();
    (window as unknown as { cytoscape: typeof factory }).cytoscape = factory;
    mockGraphDataFetch();

    const { unmount } = render(
      <MiniGraph graphDataURL="/graph/data?node=wiki/index.md&depth=1" focusNode="wiki/index.md" />,
    );
    await waitFor(() => expect(factory).toHaveBeenCalledTimes(1));

    unmount();
    expect(destroy).toHaveBeenCalledTimes(1);
    restoreAppend();
  });
});
