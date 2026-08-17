// Nav tree shapes — mirrors the /api/nav response
// (atomic/internal/serve/api_handlers.go: apiNavResponse/navGroupJSON/navNodeJSON).
export interface NavNode {
  label: string;
  relpath?: string;
  stale?: boolean;
  children?: NavNode[];
}

export interface NavGroup {
  name: string;
  // Go's encoding/json marshals a nil []navNodeJSON slice as JSON null (the
  // repo-scope "Code" group placeholder), not an empty array.
  items: NavNode[] | null;
}

export interface NavResponse {
  scope: "realm" | "repo";
  /** Directory name of the resolved scope root — what the header's chip shows. */
  name: string;
  /** Checked-out git branch at that root; empty when there isn't one. */
  branch: string;
  groups: NavGroup[];
}

// navNodeHref resolves a leaf's relpath to its React Router URL. "external"
// is the one leaf that is a dedicated screen route rather than a /page/*
// markdown target (nav.go's Group 6 comment).
export function navNodeHref(relpath: string): string {
  return relpath === "external" ? "/external" : `/page/${relpath}`;
}
