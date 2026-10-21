import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { __resetLoadScriptCacheForTest } from "./loadScript";
import { mountMermaid, rethemeMermaid } from "./mermaid";
import { __resetElkForTest, registerElkLayouts } from "./mermaidElk";

const ELK = '%%{init: {"layout": "elk"}}%%';

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

interface FakeOptions {
  // Sources containing this text fail when they carry the ELK directive, as an
  // ELK-only failure does.
  failsUnderElk?: string;
}

// A stand-in for window.mermaid: detects the type from the first word, and
// "renders" by replacing each node's text with an svg, recording the text it saw.
function installMermaid({ failsUnderElk }: FakeOptions = {}) {
  const seen: string[][] = [];
  // Real mermaid registers its type detectors in initialize; before it, every
  // detectType call throws as an unknown diagram.
  let initialized = false;
  const initialize = mock((_: Record<string, unknown>) => {
    initialized = true;
  });
  // Like mermaid.run, throws after the batch when any diagram in it failed.
  const run = mock(async ({ nodes }: { nodes: ArrayLike<HTMLElement> }) => {
    const texts: string[] = [];
    let error: Error | null = null;
    for (const node of Array.from(nodes)) {
      const text = node.textContent ?? "";
      texts.push(text);
      const broken = failsUnderElk !== undefined && text.includes(failsUnderElk) && text.startsWith(ELK);
      node.dataset.processed = "true";
      node.innerHTML = broken ? "<svg></svg>" : '<svg aria-roledescription="flowchart-v2" style="max-width: 100px"></svg>';
      if (broken) error = new Error("nt[l] is not a function");
    }
    seen.push(texts);
    if (error) throw error;
  });
  const detectType = (text: string) => {
    if (!initialized) throw new Error("No diagram type detected");
    const word = text.trim().split(/\s/)[0] ?? "";
    return word === "graph" || word === "flowchart" ? "flowchart-v2" : word;
  };
  (window as unknown as { mermaid: unknown }).mermaid = {
    initialize,
    run,
    registerLayoutLoaders: () => {},
    detectType,
  };
  return { initialize, run, seen };
}

function containerWith(...sources: string[]): HTMLElement {
  // Built via DOM APIs rather than innerHTML — happy-dom's HTML parser treats a
  // bare "-->" in source text as a stray comment-close token.
  const container = document.createElement("div");
  for (const src of sources) {
    const pre = document.createElement("pre");
    pre.className = "mermaid";
    pre.textContent = src;
    container.appendChild(pre);
  }
  document.body.appendChild(container);
  return container;
}

// The default importer requests the module over HTTP, which under test can reach
// another file's fetch mock and never settle, so every test picks its importer.
function elkAvailable() {
  __resetElkForTest();
  return registerElkLayouts(() => {}, async () => ({ default: [{ name: "elk" }] }));
}

function elkUnavailable() {
  __resetElkForTest();
  return registerElkLayouts(() => {}, async () => {
    throw new Error("404");
  });
}

let restoreScripts: () => void = () => {};

beforeEach(() => {
  void elkUnavailable();
  restoreScripts = stubScriptLoad();
});

afterEach(() => {
  restoreScripts();
  __resetLoadScriptCacheForTest();
  __resetElkForTest();
  delete (window as { mermaid?: unknown }).mermaid;
  document.head.innerHTML = "";
  document.body.innerHTML = "";
});

describe("mountMermaid", () => {
  test("no-ops when the container has no fenced mermaid blocks", async () => {
    const container = document.createElement("div");
    container.innerHTML = "<p>no diagrams</p>";
    await mountMermaid(container);
    expect(document.querySelectorAll('script[src="/vendor/mermaid.min.js"]').length).toBe(0);
  });

  test("stashes each diagram's source and renders only this container's nodes", async () => {
    const { run } = installMermaid();
    containerWith("graph TD; Z-->Y;");
    const container = containerWith("graph TD; A-->B;");
    await mountMermaid(container);

    expect(container.querySelector<HTMLElement>("pre.mermaid")?.dataset.mermaidSrc).toBe("graph TD; A-->B;");
    expect(run).toHaveBeenCalledTimes(1);
    expect(run.mock.calls[0]?.[0].nodes.length).toBe(1);
  });

  test("routes edges with ELK once its layout module has registered", async () => {
    const { seen } = installMermaid();
    await elkAvailable();
    await mountMermaid(containerWith("graph TD; A-->B;"));
    expect(seen).toEqual([[`${ELK}\ngraph TD; A-->B;`]]);
  });

  test("renders without ELK when its module cannot load, rather than not rendering", async () => {
    const { seen } = installMermaid();
    await elkUnavailable();
    await mountMermaid(containerWith("graph TD; A-->B;"));
    expect(seen).toEqual([["graph TD; A-->B;"]]);
  });

  // Mindmap throws under ELK and turns into a layered tree under dagre, so any
  // type ELK does not handle renders with no layout key at all.
  test("leaves diagram types ELK does not handle on their own layout", async () => {
    const { initialize, seen } = installMermaid();
    await elkAvailable();
    await mountMermaid(containerWith("graph TD; A-->B;", "mindmap\n  root"));
    expect(seen).toEqual([[`${ELK}\ngraph TD; A-->B;`], ["mindmap\n  root"]]);
    expect(initialize.mock.calls[0]?.[0]).not.toHaveProperty("layout");
  });

  // A note on a composite state throws under ELK, and the failed attempt can
  // leave a half-drawn svg with no error marker, so the throw is the signal.
  test("renders a diagram again without ELK when ELK fails on it", async () => {
    const { seen } = installMermaid({ failsUnderElk: "note right of" });
    await elkAvailable();
    const container = containerWith("stateDiagram\n note right of Comp : x", "graph TD; A-->B;");
    await mountMermaid(container);

    expect(seen).toEqual([
      [`${ELK}\nstateDiagram\n note right of Comp : x`],
      ["stateDiagram\n note right of Comp : x"],
      [`${ELK}\ngraph TD; A-->B;`],
    ]);
    expect(container.querySelectorAll('svg[aria-roledescription="flowchart-v2"]').length).toBe(2);
  });

  test("styles every node with a black outline, a cycled bright fill, and a dark shadow", async () => {
    const { initialize } = installMermaid();
    await mountMermaid(containerWith("graph TD; A-->B;"));
    const config = initialize.mock.calls[0]?.[0];
    expect(config?.theme).toBe("base");
    const css = String(config?.themeCSS);
    expect(css).toContain("stroke-width: 3px");
    expect(css).toContain("drop-shadow(");
    expect(css).toContain("#ff6b6b");
    expect(config?.themeVariables).toMatchObject({ nodeBorder: "#000000", primaryTextColor: "#000000" });
  });

  // The canvas padding is taken out of mermaid's inline max-width under
  // border-box sizing; without the extra room every diagram renders shrunk.
  test("widens each diagram's width cap by the canvas padding", async () => {
    installMermaid();
    const container = containerWith("graph TD; A-->B;");
    await mountMermaid(container);
    expect(container.querySelector("svg")?.style.maxWidth).toBe("124px");
  });

  // A screen reader's list of a page's buttons has to tell the diagrams apart.
  test("gives each rendered diagram its own keyboard-reachable button for the viewer", async () => {
    installMermaid();
    const container = containerWith("graph TD; A-->B;", "graph TD; C-->D;");
    await mountMermaid(container);
    const names = [...container.querySelectorAll("pre.mermaid button.mermaid-open")].map((b) => b.textContent);
    expect(names).toEqual(["Open diagram 1 in viewer", "Open diagram 2 in viewer"]);
  });
});

describe("rethemeMermaid", () => {
  test("no-ops with nothing previously mounted", async () => {
    const { run } = installMermaid();
    await rethemeMermaid();
    expect(run).not.toHaveBeenCalled();
  });

  test("renders every mounted diagram again from its stashed source", async () => {
    const { initialize, run, seen } = installMermaid();
    await elkAvailable();
    const pre = document.createElement("pre");
    pre.className = "mermaid";
    pre.dataset.mermaidSrc = "graph TD; A --- B;";
    pre.dataset.processed = "true";
    pre.innerHTML = "<svg></svg>";
    document.body.appendChild(pre);

    await rethemeMermaid();

    expect(initialize).toHaveBeenCalledTimes(1);
    expect(run).toHaveBeenCalledTimes(1);
    expect(seen[0]).toEqual([`${ELK}\ngraph TD; A --- B;`]);
  });
});
