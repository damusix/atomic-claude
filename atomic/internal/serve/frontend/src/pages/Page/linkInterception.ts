// linkInterception — classifies a click inside the server-rendered page body
// (dangerouslySetInnerHTML content) into the link forms wikilink.go/render.go
// emit, and decides whether React Router should intercept it:
//
//   - internal page link  (<a href="/page/…">, incl. the ambiguous wikilink
//     variant, class="wikilink wikilink-ambiguous")           → intercept, SPA-navigate
//   - broken wikilink     (<span class="wikilink-broken">, no href)  → not a link, no-op
//   - external link       (target="_blank", real http(s)/mailto URL) → default browser nav
//   - codeFile link       (<a href="/file/…">, no CP9 code modal yet) → default browser nav
//
// A pure function over the clicked element so it's testable without mounting
// a router; Page.tsx wires it to a delegated onClick + preventDefault.
export type PageLinkAction = { kind: "navigate"; relpath: string } | { kind: "default" };

const PAGE_HREF_PREFIX = "/page/";

export function resolvePageLinkAction(target: EventTarget | null): PageLinkAction {
  if (!(target instanceof Element)) return { kind: "default" };

  const anchor = target.closest("a[href]");
  if (!anchor) return { kind: "default" };

  const href = anchor.getAttribute("href") ?? "";
  if (!href.startsWith(PAGE_HREF_PREFIX)) return { kind: "default" };

  return { kind: "navigate", relpath: decodeURIComponent(href.slice(PAGE_HREF_PREFIX.length)) };
}
