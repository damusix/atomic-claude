// searchItems — reshapes the raw /api/search/md and /api/code/search
// responses into the flat, selectable list SearchPalette's Ark Combobox
// collection is built from.
import type { ApiCodeSearchResponse, ApiMdSearchResponse } from "./types";

export interface PaletteItem {
  id: string;
  kind: "md" | "code";
  label: string;
  sub: string;
  relpath?: string;
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

export function codePaletteItems(res: ApiCodeSearchResponse | null): PaletteItem[] {
  if (!res) return [];
  return res.members.flatMap((m) =>
    m.results.map((n) => ({
      id: `code:${m.key}:${n.id}`,
      kind: "code",
      label: n.name,
      sub: `${m.prefix} · ${n.filePath}:${n.startLine}`,
    })),
  );
}
