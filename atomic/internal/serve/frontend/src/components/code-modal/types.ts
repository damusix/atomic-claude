// Wire shapes — mirrors atomic/internal/serve/codeexplorer.go's apiCodeNode*
// / apiCodeSubgraph* / apiCodeFile* types and api_handlers.go's
// apiFileResponse (chroma-highlighted source).
export interface ApiCodeNode {
  id: string;
  name: string;
  kind: string;
  filePath: string;
  startLine: number;
  signature?: string;
  language?: string;
  docstring?: string;
}

export interface ApiCodeNodeResponse {
  member: string;
  node: ApiCodeNode;
}

export interface ApiCodeEdge {
  kind: string;
  source: string;
  target: string;
}

export interface ApiCodeSubgraphResponse {
  member: string;
  root: ApiCodeNode;
  edges: ApiCodeEdge[];
  nodes: Record<string, ApiCodeNode>;
}

export interface ApiCodeFileNode {
  id: string;
  name: string;
  kind: string;
  startLine: number;
}

export interface ApiCodeFileResponse {
  path: string;
  member?: string;
  nodes?: ApiCodeFileNode[];
  degraded?: string;
}

// apiFileResponse — GET /api/file/<relpath>, the chroma line-table HTML
// source pane (render.go:chromaHighlightLines, per-line id="L<n>" anchors).
export interface ApiSourceFileResponse {
  html: string;
  title: string;
  path: string;
}

export type SubgraphMode = "callers" | "callees" | "impact";

// IntelTarget describes what the intel pane is currently showing — the
// request the pane fetches and the drill actions that push a new one.
export type IntelTarget =
  | { kind: "file"; path: string }
  | { kind: "node"; id: string; member: string }
  | { kind: "subgraph"; id: string; member: string; mode: SubgraphMode };

export type IntelData =
  | ({ kind: "file" } & ApiCodeFileResponse)
  | ({ kind: "node" } & ApiCodeNodeResponse)
  | ({ kind: "subgraph" } & ApiCodeSubgraphResponse);

// intelUrl builds the /code/* query path for an IntelTarget — mirrors
// templates/layout.html's openModal/openCodeNode URL construction
// (/code/file?path=, /code/node?id=&member=, /code/{callers,callees,impact}?id=&member=).
export function intelUrl(target: IntelTarget): string {
  switch (target.kind) {
    case "file":
      return `/code/file?path=${encodeURIComponent(target.path)}`;
    case "node": {
      const mq = target.member ? `&member=${encodeURIComponent(target.member)}` : "";
      return `/code/node?id=${encodeURIComponent(target.id)}${mq}`;
    }
    case "subgraph": {
      const mq = target.member ? `&member=${encodeURIComponent(target.member)}` : "";
      return `/code/${target.mode}?id=${encodeURIComponent(target.id)}${mq}`;
    }
  }
}

// joinMemberPath mirrors code_members.go's joinMemberPath — a member-relative
// path (as stored in that member's own index) prefixed with the member's
// realm-relative Prefix, producing the realm-relative path /file/ and
// /api/file/ serve. An empty member (single-repo scope) returns rel unchanged.
export function joinMemberPath(member: string, rel: string): string {
  return member ? `${member}/${rel}` : rel;
}
