// Edge presentation helpers for the inspector.
//
// /api/rail returns each outbound link exactly as it was authored, so a page
// that reaches the same target through several relative prefixes (../spec/x.md
// and ../../docs/spec/x.md) yields several edges pointing at one resolved
// file, and the raw specifier — "../../.." for a bare directory link — is not
// a usable label. Dedupe on the resolved path and label from it instead.
import type { RailEdge } from "./types";

export interface EdgeView {
  edge: RailEdge;
  /** Filename — what the reader is actually looking for. */
  name: string;
  /** Parent directory, shown muted after the name to disambiguate same-named files. */
  context: string;
  /** "#section" when the link targets one, so following it from the rail
      lands where following it from the body would. */
  anchor: string;
}

/** The resolved path is anchor-free — the graph strips the fragment before
    resolving — so it is recovered from the authored target. */
function anchorOf(target: string): string {
  const hash = target.indexOf("#");
  return hash > 0 ? target.slice(hash) : "";
}

export function edgeView(edge: RailEdge): EdgeView {
  const raw = edge.resolvedPath || edge.target;
  const anchor = anchorOf(edge.target);

  if (edge.external) {
    // Hostname carries more than a truncated URL does.
    const [host] = raw.replace(/^[a-z]+:\/\//i, "").split("/");
    return { edge, name: host || raw, context: "", anchor: "" };
  }

  const segments = raw.split("/").filter((s) => s && s !== "." && s !== "..");
  const last = segments[segments.length - 1];
  const parent = segments[segments.length - 2] ?? "";

  // A purely relative directory link ("../../..") has no nameable segment.
  // Showing the raw dots reads as a rendering fault, so name it for what it
  // points at.
  if (!last) return { edge, name: "repository root", context: "", anchor };

  // Filenames keep their extension: .md, .go and .ts are what distinguishes
  // two same-named entries, and stripping it makes a file look like a folder.
  return { edge, name: edge.dir ? `${last}/` : last, context: parent, anchor };
}

/** Backlinks arrive as plain resolved paths — same presentation, no edge. */
export function backlinkView(path: string) {
  const segments = path.split("/").filter(Boolean);
  const last = segments[segments.length - 1] ?? path;
  return { name: last, context: segments[segments.length - 2] ?? "" };
}

/** The filter bucket a link falls in: its extension, or a bucket for the two
    kinds that have no meaningful one. Extensionless files (Makefile,
    Dockerfile) group together rather than each becoming its own bucket. */
export function pathKind(path: string): string {
  const base = path.split("/").filter(Boolean).pop() ?? path;
  const dot = base.lastIndexOf(".");
  return dot > 0 ? base.slice(dot + 1).toLowerCase() : "file";
}

export function edgeKind(edge: RailEdge): string {
  if (edge.external) return "link";
  if (edge.dir) return "folder";
  return pathKind(edge.resolvedPath || edge.target);
}

/** Case-insensitive substring match over everything the row displays plus the
    full path behind it — searching "serve" should find atomic/internal/serve
    even though the row only shows the filename. */
export function edgeMatches(view: EdgeView, query: string): boolean {
  if (!query) return true;
  const q = query.toLowerCase();
  const haystack = `${view.name} ${view.context} ${view.edge.resolvedPath} ${view.edge.target}`;
  return haystack.toLowerCase().includes(q);
}

/** An anchor-only link ("#section") is a jump within the page being viewed,
    not a link to another document — the on-page contents covers those. It
    also carries no resolved path, so listing it produced a row linking to
    "/page/" with nothing after it. */
function isSamePageAnchor(edge: RailEdge): boolean {
  return edge.target.startsWith("#");
}

/** Collapses edges that resolve to the same file, preserving first-seen order. */
export function dedupeEdges(edges: RailEdge[]): EdgeView[] {
  const seen = new Set<string>();
  const views: EdgeView[] = [];

  for (const edge of edges) {
    if (isSamePageAnchor(edge)) continue;
    const key = edge.resolvedPath || edge.target;
    if (seen.has(key)) continue;
    seen.add(key);
    views.push(edgeView(edge));
  }

  return views;
}

export interface EdgeCounts {
  total: number;
  code: number;
  broken: number;
  external: number;
}

export function countEdges(views: EdgeView[]): EdgeCounts {
  return {
    total: views.length,
    code: views.filter((v) => v.edge.codeFile).length,
    broken: views.filter((v) => v.edge.broken).length,
    external: views.filter((v) => v.edge.external).length,
  };
}
