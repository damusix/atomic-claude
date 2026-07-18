// Search wire shapes — mirrors atomic/internal/serve/api_handlers.go's
// search-endpoint responses (apiMdSearchResponse, apiCodeSearchResponse,
// apiSearchStreamCodeEvent) shared by the palette (SearchPalette) and the
// dedicated /search page (pages/Search).
export interface ApiMdSearchResult {
  relpath: string;
  line: number;
  snippet: string;
}

export interface ApiMdSearchResponse {
  query: string;
  truncated: boolean;
  cap: number;
  results: ApiMdSearchResult[];
}

export interface ApiNodeRef {
  id: string;
  name: string;
  kind: string;
  filePath: string;
  startLine: number;
}

export interface ApiCodeSearchMember {
  key: string;
  prefix: string;
  indexed: boolean;
  results: ApiNodeRef[];
}

export interface ApiCodeSearchResponse {
  members: ApiCodeSearchMember[];
}

export interface ApiSearchStreamMemberInfo {
  key: string;
  prefix: string;
  indexed: boolean;
}

export interface ApiSearchStreamCodeEvent {
  member: ApiSearchStreamMemberInfo;
  results: ApiNodeRef[];
}

export type SearchSrc = "all" | "md" | "code";

// normalizeSearchSrc mirrors search_page.go's normalizeSearchSrc — clamps an
// arbitrary src param to a known value, defaulting to "all".
export function normalizeSearchSrc(src: string | null | undefined): SearchSrc {
  return src === "md" || src === "code" ? src : "all";
}
