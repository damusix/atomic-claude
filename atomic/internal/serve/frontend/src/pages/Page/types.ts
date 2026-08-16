// Page shapes — mirrors the /api/page response
// (atomic/internal/serve/api_handlers.go: apiPageResponse / apiPageDirResponse
// / dirEntry / breadcrumbSeg).
export interface BreadcrumbSeg {
  label: string;
  path?: string;
  folder?: boolean;
}

export interface PageResponse {
  html: string;
  title: string;
  relpath: string;
  hasMermaid: boolean;
  breadcrumb: BreadcrumbSeg[];
}

export interface DirEntryMeta {
  /** On-disk name including extension; `name` has it stripped for display. */
  filename?: string;
  /** Frontmatter title, else the document's own first heading. */
  title?: string;
  /** Frontmatter description, else the opening prose, capped at ~150 chars. */
  summary?: string;
  /** For a folder: the index file it opens, empty when it has none. */
  index?: string;
}

export interface DirEntry extends DirEntryMeta {
  name: string;
  relpath: string;
  folder: boolean;
}

export interface PageDirResponse {
  dir: true;
  relpath: string;
  entries: DirEntry[];
}

export function isDirResponse(data: PageResponse | PageDirResponse): data is PageDirResponse {
  return "dir" in data && data.dir === true;
}
