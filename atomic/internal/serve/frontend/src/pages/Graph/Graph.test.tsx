import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { __resetGraphEngineLoadedForTest } from "../../utils/graphEngineAdapter";
import { __resetLoadScriptCacheForTest } from "../../utils/loadScript";
import { __resetForTest } from "../../utils/memberStore";
import { Graph } from "./Graph";

const NAV_FIXTURE = { scope: "realm", name: "acme", branch: "", groups: [] };

function seedMemberCookie(member: string) {
  document.cookie = `atomic-member=${encodeURIComponent(JSON.stringify({ "realm:acme": member }))}; path=/`;
}

// Stubs the same way graphEngineAdapter.test.ts / loadScript.test.ts do —
// happy-dom throws on real script-file loading.
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

function stubGraphEngine() {
  const systemMount = mock(() => {});
  const systemTeardown = mock(() => {});
  const codeMount = mock(() => {});
  const codeTeardown = mock(() => {});
  window.SystemGraph = { mount: systemMount, teardown: systemTeardown, retheme: mock(() => {}) };
  window.CodeGraph = { mount: codeMount, teardown: codeTeardown, retheme: mock(() => {}) };
  return { systemMount, systemTeardown, codeMount, codeTeardown };
}

function mockFetchByUrl(handlers: Record<string, unknown>) {
  const withNav = { "/nav": NAV_FIXTURE, ...handlers };
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    for (const [match, body] of Object.entries(withNav)) {
      if (url.includes(match)) {
        return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
      }
    }
    return new Response(JSON.stringify({ error: "unexpected path" }), { status: 404 });
  }) as unknown as typeof fetch;
}

function stubMembersFetch(members: Array<{ prefix: string; indexed: boolean }>) {
  mockFetchByUrl({ "/code/graph/members": { members } });
}

function renderAt(path: string) {
  const router = createMemoryRouter([{ path: "/graph", element: <Graph /> }], { initialEntries: [path] });
  const result = render(<RouterProvider router={router} />);
  return { router, ...result };
}

describe("Graph route", () => {
  afterEach(() => {
    __resetLoadScriptCacheForTest();
    __resetGraphEngineLoadedForTest();
    document.head.innerHTML = "";
    delete window.SystemGraph;
    delete window.CodeGraph;
    // @ts-expect-error — test-only global cleanup
    delete globalThis.fetch;
    __resetForTest();
    document.cookie = "atomic-member=; path=/; max-age=0";
  });

  test("defaults to the docs view and mounts window.SystemGraph", async () => {
    const restoreScripts = stubScriptLoad();
    const engine = stubGraphEngine();
    mockFetchByUrl({});
    renderAt("/graph");

    await waitFor(() => expect(engine.systemMount).toHaveBeenCalledTimes(1));
    expect(screen.getByRole("button", { name: "Docs" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Code" })).toHaveAttribute("aria-pressed", "false");
    restoreScripts();
  });

  // The rail and the reading padding are collapsed by app.css's
  // mode-graph-view rules. This route claims that class, rather than
  // graph-core.js's engine mount claiming it, because the background layout
  // warm mounts that same engine into an offscreen container from ordinary
  // pages: "the engine is running" and "the graph owns the screen" stopped
  // being the same statement, and only the second one is a layout fact.
  test("claims the full-pane layout while open and gives it back on leave", async () => {
    const restoreScripts = stubScriptLoad();
    const engine = stubGraphEngine();
    mockFetchByUrl({});
    const { unmount } = renderAt("/graph");

    // Asserted before the engine has finished mounting: the layout must be
    // claimed by the time the container is measured, not after.
    expect(document.body.classList.contains("mode-graph-view")).toBe(true);
    await waitFor(() => expect(engine.systemMount).toHaveBeenCalledTimes(1));

    unmount();
    expect(document.body.classList.contains("mode-graph-view")).toBe(false);
    restoreScripts();
  });

  test("?view=code mounts window.CodeGraph and shows no picker for a single member", async () => {
    const restoreScripts = stubScriptLoad();
    const engine = stubGraphEngine();
    stubMembersFetch([{ prefix: "", indexed: true }]);
    renderAt("/graph?view=code");

    await waitFor(() => expect(engine.codeMount).toHaveBeenCalledTimes(1));
    expect(engine.codeMount).toHaveBeenCalledWith(expect.anything(), undefined);
    expect(screen.queryByLabelText("Code member")).not.toBeInTheDocument();
    restoreScripts();
  });

  test("shows the member picker for a realm with several members and mounts the store's member", async () => {
    const restoreScripts = stubScriptLoad();
    const engine = stubGraphEngine();
    seedMemberCookie("alpha");
    stubMembersFetch([
      { prefix: "alpha", indexed: true },
      { prefix: "beta", indexed: false },
    ]);
    renderAt("/graph?view=code");

    await waitFor(() => expect(engine.codeMount).toHaveBeenCalledTimes(1));
    expect(engine.codeMount).toHaveBeenCalledWith(expect.anything(), "alpha");
    const picker = await screen.findByLabelText("Code member");
    expect(picker).toBeInTheDocument();
    expect(screen.getByRole("option", { name: "beta — not indexed" })).toBeInTheDocument();
    restoreScripts();
  });

  test("a stored member absent from Graph's member list renders the first member and leaves the cookie unchanged", async () => {
    const restoreScripts = stubScriptLoad();
    const engine = stubGraphEngine();
    seedMemberCookie("bogus");
    stubMembersFetch([
      { prefix: "alpha", indexed: true },
      { prefix: "beta", indexed: false },
    ]);
    renderAt("/graph?view=code");

    await waitFor(() => expect(engine.codeMount).toHaveBeenCalledTimes(1));
    expect(engine.codeMount).toHaveBeenCalledWith(expect.anything(), "alpha");
    expect(document.cookie).toContain(encodeURIComponent(JSON.stringify({ "realm:acme": "bogus" })));
    const [mounted] = engine.codeMount.mock.calls[0] as unknown as [HTMLElement];
    expect(mounted).toBe(document.getElementById("code-cy") as HTMLElement);
    expect(mounted.isConnected).toBe(true);
    restoreScripts();
  });

  test("the empty-prefix member renders the realm's name from the store", async () => {
    const restoreScripts = stubScriptLoad();
    const engine = stubGraphEngine();
    stubMembersFetch([
      { prefix: "", indexed: true },
      { prefix: "atomic", indexed: true },
    ]);
    renderAt("/graph?view=code");

    await waitFor(() => expect(engine.codeMount).toHaveBeenCalledTimes(1));
    const select = await screen.findByLabelText("Code member");
    expect(select.querySelector("option[value='']")).toHaveTextContent("acme");
    restoreScripts();
  });

  test("switching to the docs view tears down the code profile and mounts the docs profile", async () => {
    const restoreScripts = stubScriptLoad();
    const engine = stubGraphEngine();
    stubMembersFetch([{ prefix: "", indexed: true }]);
    const user = userEvent.setup();
    renderAt("/graph?view=code");

    await waitFor(() => expect(engine.codeMount).toHaveBeenCalledTimes(1));

    await user.click(screen.getByRole("button", { name: "Docs" }));

    await waitFor(() => expect(engine.codeTeardown).toHaveBeenCalledTimes(1));
    await waitFor(() => expect(engine.systemMount).toHaveBeenCalledTimes(1));
    restoreScripts();
  });

  test("picking a member in the picker calls the store's setMember", async () => {
    const restoreScripts = stubScriptLoad();
    const engine = stubGraphEngine();
    stubMembersFetch([
      { prefix: "alpha", indexed: true },
      { prefix: "beta", indexed: false },
    ]);
    const user = userEvent.setup();
    renderAt("/graph?view=code");

    const picker = await screen.findByLabelText("Code member");
    await user.selectOptions(picker, "beta");

    await waitFor(() => expect(engine.codeMount).toHaveBeenLastCalledWith(expect.anything(), "beta"));
    expect(document.cookie).toContain(encodeURIComponent(JSON.stringify({ "realm:acme": "beta" })));
    restoreScripts();
  });
});
