// Rail shapes — mirrors the /api/rail response
// (atomic/internal/serve/api_handlers.go: apiRailResponse) and its embedded
// Edge shape (graph.go).
export interface PropKV {
  key: string;
  value: string;
  isURL: boolean;
  isJSON: boolean;
}

export interface RailEdge {
  target: string;
  resolvedPath: string;
  broken: boolean;
  /** Resolved to a directory with no index file — a listing page, not a file. */
  dir: boolean;
  ambiguous: boolean;
  codeFile: boolean;
  external: boolean;
}

export interface RailBacklink {
  path: string;
}

export interface RailResponse {
  relpath: string;
  orphan: boolean;
  properties: PropKV[] | null;
  out: RailEdge[] | null;
  in: RailBacklink[] | null;
  graphDataURL: string;
}
