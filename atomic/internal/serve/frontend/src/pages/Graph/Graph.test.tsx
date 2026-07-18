import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { __resetGraphEngineLoadedForTest } from "../../utils/graphEngineAdapter";
import { __resetLoadScriptCacheForTest } from "../../utils/loadScript";
import { Graph } from "./Graph";

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

function stubMembersFetch(members: Array<{ prefix: string; indexed: boolean }>) {
  globalThis.fetch = mock(() =>
    Promise.resolve(
      new Response(JSON.stringify({ members }), { status: 200, headers: { "Content-Type": "application/json" } }),
    ),
  ) as unknown as typeof fetch;
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
  });

  test("defaults to the docs view and mounts window.SystemGraph", async () => {
    const restoreScripts = stubScriptLoad();
    const engine = stubGraphEngine();
    renderAt("/graph");

    await waitFor(() => expect(engine.systemMount).toHaveBeenCalledTimes(1));
    expect(screen.getByRole("button", { name: "Docs" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Code" })).toHaveAttribute("aria-pressed", "false");
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

  test("shows the member picker for a realm with several members and mounts the resolved member", async () => {
    const restoreScripts = stubScriptLoad();
    const engine = stubGraphEngine();
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

  test("an unrecognized ?member= among several falls back to the first member and rewrites the URL", async () => {
    const restoreScripts = stubScriptLoad();
    const engine = stubGraphEngine();
    stubMembersFetch([
      { prefix: "alpha", indexed: true },
      { prefix: "beta", indexed: false },
    ]);
    const { router } = renderAt("/graph?view=code&member=bogus");

    await waitFor(() => expect(engine.codeMount).toHaveBeenCalledTimes(1));
    expect(engine.codeMount).toHaveBeenCalledWith(expect.anything(), "alpha");
    expect(router.state.location.search).toContain("member=alpha");
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
});
