import { describe, expect, test } from "bun:test";
import { resolvePageLinkAction } from "./linkInterception";

// Each fixture mirrors one link form wikilink.go/render.go actually emit —
// see the module comment for the exact classes/attrs.
function el(html: string): Element {
  const wrap = document.createElement("div");
  wrap.innerHTML = html;
  return wrap.firstElementChild as Element;
}

describe("resolvePageLinkAction", () => {
  test("internal resolved page link -> navigate with the decoded relpath", () => {
    const a = el('<a class="wikilink" href="/page/wiki/index.md">index</a>');
    expect(resolvePageLinkAction(a)).toEqual({ kind: "navigate", relpath: "wiki/index.md" });
  });

  test("ambiguous wikilink (still an <a href=\"/page/...\">) -> navigate", () => {
    const a = el(
      '<a class="wikilink wikilink-ambiguous" href="/page/notes.md" title="ambiguous: multiple files match">notes</a>',
    );
    expect(resolvePageLinkAction(a)).toEqual({ kind: "navigate", relpath: "notes.md" });
  });

  test("broken wikilink is a <span>, not a link -> default (no-op)", () => {
    const span = el('<span class="wikilink-broken" title="unresolved wikilink: gone">gone</span>');
    expect(resolvePageLinkAction(span)).toEqual({ kind: "default" });
  });

  test("external link -> default (browser handles new-tab nav)", () => {
    const a = el('<a href="https://example.com" target="_blank" rel="noopener noreferrer">ext</a>');
    expect(resolvePageLinkAction(a)).toEqual({ kind: "default" });
  });

  test("codeFile link (/file/...) -> open the code modal", () => {
    const a = el('<a href="/file/atomic/internal/serve/render.go">render.go</a>');
    expect(resolvePageLinkAction(a)).toEqual({
      kind: "code",
      path: "atomic/internal/serve/render.go",
      line: null,
    });
  });

  test("codeFile link with a #L<n> anchor -> open the code modal at that line", () => {
    const a = el('<a href="/file/atomic/internal/serve/render.go#L42">render.go:42</a>');
    expect(resolvePageLinkAction(a)).toEqual({
      kind: "code",
      path: "atomic/internal/serve/render.go",
      line: 42,
    });
  });

  test("click on nested inline content resolves via closest('a[href]')", () => {
    const a = el('<a class="wikilink" href="/page/notes.md"><code>notes</code></a>');
    const inner = a.querySelector("code") as Element;
    expect(resolvePageLinkAction(inner)).toEqual({ kind: "navigate", relpath: "notes.md" });
  });

  test("click outside any anchor -> default", () => {
    const p = el("<p>plain text</p>");
    expect(resolvePageLinkAction(p)).toEqual({ kind: "default" });
  });

  test("non-Element target -> default", () => {
    expect(resolvePageLinkAction(null)).toEqual({ kind: "default" });
  });
});
