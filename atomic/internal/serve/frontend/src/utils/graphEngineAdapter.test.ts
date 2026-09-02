import { afterEach, describe, expect, mock, test } from "bun:test";
import {
  __resetGraphEngineLoadedForTest,
  ensureGraphEngineLoaded,
  fetchGraphMembers,
  type GraphMember,
  mountGraph,
  pickerLabel,
  resolveMember,
  rethemeGraph,
  teardownGraph,
} from "./graphEngineAdapter";
import { __resetLoadScriptCacheForTest } from "./loadScript";

// Stubs document.createElement("script") the same way loadScript.test.ts
// does — happy-dom throws on real script-file loading, and a unit test has
// no business fetching the real vendor bundles.
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

describe("graphEngineAdapter", () => {
  afterEach(() => {
    __resetLoadScriptCacheForTest();
    __resetGraphEngineLoadedForTest();
    document.head.innerHTML = "";
    delete window.SystemGraph;
    delete window.CodeGraph;
    delete window.GraphCore;
    // @ts-expect-error — test-only global cleanup
    delete globalThis.fetch;
  });

  test("ensureGraphEngineLoaded appends the vendor, core, and both profile scripts", async () => {
    const restore = stubScriptLoad();
    await ensureGraphEngineLoaded();
    const srcs = Array.from(document.head.querySelectorAll("*")).map((el) => (el as HTMLScriptElement).src);
    expect(srcs.some((s) => s.endsWith("/vendor/cosmos-graph.js"))).toBe(true);
    expect(srcs.some((s) => s.endsWith("/graph-core.js"))).toBe(true);
    expect(srcs.some((s) => s.endsWith("/system-graph.js"))).toBe(true);
    expect(srcs.some((s) => s.endsWith("/code-graph.js"))).toBe(true);
    restore();
  });

  test("mountGraph loads the engine then delegates to window.SystemGraph.mount for the docs view, which calls window.GraphCore.mount", async () => {
    const restore = stubScriptLoad();
    const graphCoreMount = mock((_container: HTMLElement, _profile: unknown) => {});
    // window.SystemGraph.mount is what the carried system-graph.js exports —
    // it internally calls window.GraphCore.mount(container, docsProfile).
    // The stub reproduces that one-line delegation so the assertion below
    // proves the adapter's call reaches the actual engine contract, not just
    // a mocked wrapper.
    window.GraphCore = { mount: graphCoreMount, teardown: mock(() => {}), retheme: mock(() => {}) };
    window.SystemGraph = {
      mount: (container: HTMLElement) => window.GraphCore?.mount(container, {}),
      teardown: () => window.GraphCore?.teardown(),
      retheme: () => window.GraphCore?.retheme(),
    };

    const container = document.createElement("div");
    await mountGraph(container, "docs");

    expect(graphCoreMount).toHaveBeenCalledTimes(1);
    expect(graphCoreMount.mock.calls[0]?.[0]).toBe(container);
    restore();
  });

  test("mountGraph delegates to window.CodeGraph.mount with the member for the code view", async () => {
    const restore = stubScriptLoad();
    const codeGraphMount = mock(() => {});
    window.CodeGraph = { mount: codeGraphMount, teardown: mock(() => {}), retheme: mock(() => {}) };

    const container = document.createElement("div");
    await mountGraph(container, "code", "member-a");

    expect(codeGraphMount).toHaveBeenCalledWith(container, "member-a");
    restore();
  });

  test("teardownGraph calls the matching profile's teardown", () => {
    const systemTeardown = mock(() => {});
    const codeTeardown = mock(() => {});
    window.SystemGraph = { mount: mock(() => {}), teardown: systemTeardown, retheme: mock(() => {}) };
    window.CodeGraph = { mount: mock(() => {}), teardown: codeTeardown, retheme: mock(() => {}) };

    teardownGraph("docs");
    expect(systemTeardown).toHaveBeenCalledTimes(1);
    expect(codeTeardown).not.toHaveBeenCalled();

    teardownGraph("code");
    expect(codeTeardown).toHaveBeenCalledTimes(1);
  });

  test("rethemeGraph delegates to window.GraphCore.retheme when a graph is mounted", () => {
    const retheme = mock(() => {});
    window.GraphCore = { mount: mock(() => {}), teardown: mock(() => {}), retheme };
    rethemeGraph();
    expect(retheme).toHaveBeenCalledTimes(1);
  });

  test("rethemeGraph no-ops with no engine loaded", () => {
    expect(() => rethemeGraph()).not.toThrow();
  });

  test("resolveMember keeps the request when there's one member or fewer", () => {
    expect(resolveMember([], "anything")).toBe("anything");
    expect(resolveMember([{ prefix: "solo", indexed: true }], "solo")).toBe("solo");
    expect(resolveMember([{ prefix: "solo", indexed: true }], "")).toBe("");
  });

  test("resolveMember falls back to the first member when the requested one is unrecognized among several", () => {
    const members: GraphMember[] = [
      { prefix: "alpha", indexed: true },
      { prefix: "beta", indexed: false },
    ];
    expect(resolveMember(members, "bogus")).toBe("alpha");
    expect(resolveMember(members, "beta")).toBe("beta");
  });

  test("fetchGraphMembers returns the parsed member list on success", async () => {
    globalThis.fetch = mock(() =>
      Promise.resolve(
        new Response(JSON.stringify({ members: [{ prefix: "alpha", indexed: true }] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    ) as unknown as typeof fetch;

    const members = await fetchGraphMembers();
    expect(members).toEqual([{ prefix: "alpha", indexed: true }]);
  });

  test("fetchGraphMembers returns an empty list on a transport failure", async () => {
    globalThis.fetch = mock(() => Promise.reject(new Error("network down"))) as unknown as typeof fetch;
    const members = await fetchGraphMembers();
    expect(members).toEqual([]);
  });

  test("pickerLabel appends the not-indexed suffix for an unindexed member", () => {
    expect(pickerLabel({ prefix: "alpha", indexed: false }, "acme")).toBe("alpha — not indexed");
  });

  test("pickerLabel renders the bare prefix for an indexed member", () => {
    expect(pickerLabel({ prefix: "alpha", indexed: true }, "acme")).toBe("alpha");
  });

  test("pickerLabel renders the realm name for an empty prefix", () => {
    expect(pickerLabel({ prefix: "", indexed: true }, "acme")).toBe("acme");
  });
});
