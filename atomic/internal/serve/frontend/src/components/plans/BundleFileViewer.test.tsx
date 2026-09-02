import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen } from "@testing-library/react";
import { ApiProvider } from "../../utils/api";
import { BundleFileViewer } from "./BundleFileViewer";

function mockFetchByUrl(handlers: Record<string, unknown>) {
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    for (const [match, body] of Object.entries(handlers)) {
      if (url.includes(match)) {
        return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
      }
    }
    return new Response(JSON.stringify({ error: "unexpected path" }), { status: 404 });
  }) as unknown as typeof fetch;
}

function renderViewer(kind: "markdown" | "html" | "file", relpath: string) {
  return render(
    <ApiProvider>
      <BundleFileViewer checkoutId="w-main" relpath={relpath} kind={kind} />
    </ApiProvider>,
  );
}

describe("BundleFileViewer", () => {
  afterEach(() => {
    mock.restore();
  });

  test("html kind renders a sandboxed iframe sourced with raw=1, HTML kept out of the surrounding DOM", () => {
    mockFetchByUrl({});
    renderViewer("html", "options.html");

    const iframe = screen.getByTitle("options.html") as HTMLIFrameElement;
    expect(iframe.tagName).toBe("IFRAME");
    expect(iframe.getAttribute("sandbox")).toBe("allow-scripts");
    expect(iframe.getAttribute("src")).toContain("raw=1");
    expect(iframe.getAttribute("src")).toContain("worktree=w-main");
    expect(iframe.getAttribute("src")).toContain("path=options.html");

    // The fetched HTML must never land in the app's own document tree.
    expect(document.body.innerHTML).not.toContain("<script");
    expect(document.querySelector("body > script")).toBeNull();
  });

  test("file kind renders a download link with raw=1 and no inline content", () => {
    renderViewer("file", "assets/report.pdf");

    const link = screen.getByRole("link", { name: /report\.pdf/ }) as HTMLAnchorElement;
    expect(link.getAttribute("href")).toContain("raw=1");
    expect(link.getAttribute("href")).toContain("path=assets%2Freport.pdf");
    expect(link.hasAttribute("download")).toBe(true);
    // No inline preview — the file's bytes never appear in the DOM.
    expect(screen.queryByRole("iframe")).toBeNull();
  });

  test("markdown kind fetches without raw and renders through the existing markdown pipeline", async () => {
    mockFetchByUrl({
      "/api/plans/page": {
        html: "<p>hello bundle doc</p>",
        title: "notes",
        relpath: "notes.md",
        hasMermaid: false,
        breadcrumb: [],
      },
    });
    renderViewer("markdown", "notes.md");

    await screen.findByText("hello bundle doc");

    const call = (globalThis.fetch as unknown as ReturnType<typeof mock>).mock.calls[0];
    const url = typeof call[0] === "string" ? call[0] : call[0].toString();
    expect(url).not.toContain("raw=1");
    expect(url).toContain("worktree=w-main");
    expect(url).toContain("path=notes.md");
  });
});
