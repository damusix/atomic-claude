import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { ApiProvider } from "../../utils/api";
import { events } from "../../utils/events";
import { Page } from "./Page";

function renderAt(path: string) {
  const router = createMemoryRouter(
    [{ path: "/", children: [{ path: "page/*", element: <Page /> }] }],
    { initialEntries: [path] },
  );
  return render(
    <ApiProvider>
      <RouterProvider router={router} />
    </ApiProvider>,
  );
}

function mockFetchOnce(body: unknown, status = 200) {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
  ) as unknown as typeof fetch;
}

// happy-dom's HTMLScriptElement throws synchronously on connect when real
// script-file loading is attempted (see utils/loadScript.test.ts's
// stubScriptLoad comment) — substitute a <div> for "script" tags so
// mountMermaid's loadScript() call resolves without touching the network.
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

describe("Page", () => {
  afterEach(() => {
    mock.restore();
  });

  test("skeleton renders before /api/page resolves — no blank flash", async () => {
    // A fetch that resolves only after the assertion runs — mock.restore()
    // (afterEach) only clears bun:test's mock call-tracking, it does not
    // revert a bare `globalThis.fetch = mock(...)` assignment, so a
    // never-resolving mock here would hang every subsequent test's fetch.
    const origFetch = globalThis.fetch;
    let resolvePending: (() => void) | undefined;
    globalThis.fetch = mock(
      () =>
        new Promise<Response>((resolve) => {
          resolvePending = () =>
            resolve(
              new Response(
                JSON.stringify({
                  html: "<p>ok</p>",
                  title: "README",
                  relpath: "README.md",
                  hasMermaid: false,
                  breadcrumb: [],
                }),
                { status: 200, headers: { "Content-Type": "application/json" } },
              ),
            );
        }),
    ) as unknown as typeof fetch;

    renderAt("/page/README.md");
    expect(document.querySelector(".page-skeleton")).not.toBeNull();

    // The hook fires its fetch post-mount, not synchronously during render —
    // wait for the mock to actually be invoked before resolving it.
    await waitFor(() => expect(resolvePending).toBeDefined());
    resolvePending?.();
    await waitFor(() => expect(screen.getByText("ok")).toBeInTheDocument());
    globalThis.fetch = origFetch;
  });

  test("injects the server-rendered HTML from /api/page (HTML-in-JSON)", async () => {
    mockFetchOnce({
      html: "<p>hello <strong>world</strong></p>",
      title: "README",
      relpath: "README.md",
      hasMermaid: false,
      breadcrumb: [{ label: "README.md" }],
    });

    renderAt("/page/README.md");

    await waitFor(() => expect(screen.getByText("world")).toBeInTheDocument());
    expect(document.querySelector(".page-body strong")).not.toBeNull();
  });

  test("emits page.resolved with the server-RESOLVED relpath (may differ from the URL param)", async () => {
    mockFetchOnce({
      html: "<p>index</p>",
      title: "index",
      relpath: "wiki/index.md", // directory URL resolved to its index file
      hasMermaid: false,
      breadcrumb: [],
    });

    const seen: (string | null)[] = [];
    const off = events.on("page.resolved", ({ relpath }) => seen.push(relpath));

    renderAt("/page/wiki/");

    await waitFor(() => expect(seen).toContain("wiki/index.md"));
    off();
  });

  test("directory listing renders entries and does not clear rail with a stale relpath (emits null)", async () => {
    mockFetchOnce({
      dir: true,
      relpath: "docs",
      entries: [
        { name: "guide", relpath: "docs/guide.md", folder: false },
        { name: "sub", relpath: "docs/sub/", folder: true },
      ],
    });

    const seen: (string | null)[] = [];
    const off = events.on("page.resolved", ({ relpath }) => seen.push(relpath));

    renderAt("/page/docs");

    await waitFor(() => expect(screen.getByText("guide")).toBeInTheDocument());
    expect(screen.getByText("sub/")).toBeInTheDocument();
    expect(seen).toContain(null);
    off();
  });

  test("404 renders the not-found view", async () => {
    mockFetchOnce({ error: "not found: missing.md" }, 404);

    renderAt("/page/missing.md");

    await waitFor(() => expect(screen.getByText("Not found")).toBeInTheDocument());
  });

  test("mounts mermaid when hasMermaid is true", async () => {
    const restore = stubScriptLoad();
    mockFetchOnce({
      html: '<pre class="mermaid">graph TD; A-->B;</pre>',
      title: "diagram",
      relpath: "diagram.md",
      hasMermaid: true,
      breadcrumb: [],
    });

    renderAt("/page/diagram.md");

    await waitFor(() => expect(document.querySelector("pre.mermaid")).not.toBeNull());
    // mountMermaid itself (lazy script load + mermaid.run) is covered by
    // utils/mermaid's own unit coverage; this asserts Page wires the effect
    // by giving mountMermaid a real container holding the fenced block.
    restore();
  });

  test("does not mount mermaid when hasMermaid is false", async () => {
    mockFetchOnce({
      html: "<p>no diagrams here</p>",
      title: "plain",
      relpath: "plain.md",
      hasMermaid: false,
      breadcrumb: [],
    });

    renderAt("/page/plain.md");

    await waitFor(() => expect(screen.getByText("no diagrams here")).toBeInTheDocument());
    expect(document.querySelector("pre.mermaid")).toBeNull();
  });

  test("clicking an internal page link intercepts and prevents default browser navigation", async () => {
    mockFetchOnce({
      html: '<a class="wikilink" href="/page/other.md">other</a>',
      title: "index",
      relpath: "index.md",
      hasMermaid: false,
      breadcrumb: [],
    });

    renderAt("/page/index.md");
    await waitFor(() => expect(screen.getByText("other")).toBeInTheDocument());

    const link = screen.getByText("other") as HTMLAnchorElement;
    const clickEvent = new MouseEvent("click", { bubbles: true, cancelable: true });
    link.dispatchEvent(clickEvent);

    expect(clickEvent.defaultPrevented).toBe(true);
  });

  test("clicking an external link does not intercept (default browser nav)", async () => {
    mockFetchOnce({
      html: '<a href="https://example.com" target="_blank" rel="noopener noreferrer">ext</a>',
      title: "index",
      relpath: "index.md",
      hasMermaid: false,
      breadcrumb: [],
    });

    renderAt("/page/index.md");
    await waitFor(() => expect(screen.getByText("ext")).toBeInTheDocument());

    const link = screen.getByText("ext");
    const clickEvent = new MouseEvent("click", { bubbles: true, cancelable: true });
    link.dispatchEvent(clickEvent);

    expect(clickEvent.defaultPrevented).toBe(false);
  });
});
