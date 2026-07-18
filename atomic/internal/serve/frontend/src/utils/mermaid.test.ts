import { afterEach, describe, expect, mock, test } from "bun:test";
import { __resetLoadScriptCacheForTest } from "./loadScript";
import { mountMermaid } from "./mermaid";

// See loadScript.test.ts's stubScriptLoad comment: happy-dom's
// HTMLScriptElement throws synchronously on connect when real script-file
// loading is attempted — substitute a <div> for "script" tags so
// loadScript() resolves without touching the network.
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

describe("mountMermaid", () => {
  afterEach(() => {
    __resetLoadScriptCacheForTest();
    delete (window as { mermaid?: unknown }).mermaid;
    document.head.innerHTML = "";
  });

  test("no-ops when the container has no fenced mermaid blocks", async () => {
    const restore = stubScriptLoad();
    const container = document.createElement("div");
    container.innerHTML = "<p>no diagrams</p>";
    await mountMermaid(container);
    expect(document.querySelectorAll('script[src="/vendor/mermaid.min.js"]').length).toBe(0);
    restore();
  });

  test("lazy-loads the vendor script, stashes source, initializes, and runs scoped to this container's nodes", async () => {
    const restore = stubScriptLoad();
    const run = mock(() => {});
    const initialize = mock(() => {});
    (window as unknown as { mermaid: { initialize: typeof initialize; run: typeof run } }).mermaid = {
      initialize,
      run,
    };

    // Built via DOM APIs rather than innerHTML — happy-dom's HTML parser
    // treats a bare "-->" in source text as a stray comment-close token and
    // mangles the surrounding text when assigned through innerHTML.
    const container = document.createElement("div");
    const preEl = document.createElement("pre");
    preEl.className = "mermaid";
    preEl.textContent = "graph TD; A-->B;";
    container.appendChild(preEl);
    await mountMermaid(container);

    const pre = container.querySelector("pre.mermaid") as HTMLElement;
    expect(pre.dataset.mermaidSrc).toBe("graph TD; A-->B;");
    expect(initialize).toHaveBeenCalledTimes(1);
    expect(run).toHaveBeenCalledTimes(1);
    const call = run.mock.calls[0] as unknown as [{ nodes: NodeListOf<Element> }];
    expect(call[0].nodes.length).toBe(1);
    restore();
  });
});
