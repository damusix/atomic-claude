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

export interface DirEntry {
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
