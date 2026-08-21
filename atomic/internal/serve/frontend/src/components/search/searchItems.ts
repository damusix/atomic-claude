// searchItems — reshapes the raw /api/search/md and /api/code/search
// responses, and the client-held /api/plans payload, into the flat,
// selectable list SearchPalette's Ark Combobox collection is built from.
import type { ApiCodeSearchResponse, ApiMdSearchResponse } from "./types";
import { chipsFor } from "../plans/PlansView";
import type { PlanRow } from "../../utils/plansApi";

export interface PaletteItem {
  id: string;
  kind: "md" | "code" | "plans";
  label: string;
  sub: string;
  relpath?: string;
  // Code result fields — used by the code-modal seam (CP9) to open the
  // symbol without a second lookup.
  codeId?: string;
  member?: string;
  filePath?: string;
  startLine?: number;
  // Plans result field — the slug to navigate to on select.
  slug?: string;
}

export function mdPaletteItems(res: ApiMdSearchResponse | null): PaletteItem[] {
  if (!res) return [];
  return res.results.map((r) => ({
    id: `md:${r.relpath}:${r.line}`,
    kind: "md",
    label: r.relpath,
    sub: r.snippet,
    relpath: r.relpath,
  }));
}

/**
 * Case-insensitive substring match over title, description, and slug — the
 * one rule both ⌘K's plans tab and the Plans list filter apply, so the two
 * surfaces never disagree on what matches. An empty query matches every row.
 */
export function filterPlanRows(rows: PlanRow[], query: string): PlanRow[] {
  const q = query.trim().toLowerCase();
  if (!q) return rows;
  return rows.filter(
    (r) =>
      r.title.toLowerCase().includes(q) ||
      r.description.toLowerCase().includes(q) ||
      r.slug.toLowerCase().includes(q),
  );
}

export function planPaletteItems(rows: PlanRow[] | null, query: string): PaletteItem[] {
  if (!rows) return [];
  if (!query.trim()) return [];
  const matches = filterPlanRows(rows, query);
  return matches.map((r) => ({
    id: `plans:${r.slug}`,
    kind: "plans",
    label: r.title || r.slug,
    sub: r.description || chipsFor(r).join(" · "),
    slug: r.slug,
  }));
}

export function codePaletteItems(res: ApiCodeSearchResponse | null): PaletteItem[] {
  if (!res) return [];
  return res.members.flatMap((m) =>
    m.results.map((n) => ({
      id: `code:${m.key}:${n.id}`,
      kind: "code",
      label: n.name,
      sub: `${m.prefix} · ${n.filePath}:${n.startLine}`,
      codeId: n.id,
      member: m.prefix,
      filePath: n.filePath,
      startLine: n.startLine,
    })),
  );
}
