// Page — page view for "/" (landing) and /page/<relpath>: fetches /api/page,
// injects the server-rendered HTML (dangerouslySetInnerHTML — same trust
// domain as today's server-rendered fragments, see the spec's Risks table),
// intercepts internal-page-link clicks for SPA navigation, mounts fenced
// mermaid diagrams, and renders the directory-listing / not-found fallbacks
// the same endpoint carries. Emits "page.resolved" (utils/events) so
// components/rail — mounted separately in Shell's aside — knows which
// relpath to fetch /api/rail for.
import { useEffect, useRef } from "react";
import { useNavigate, useParams } from "react-router";
import { openFile } from "../../components/code-modal/store";
import { useApi } from "../../utils/api";
import { events } from "../../utils/events";
import { mountMermaid } from "../../utils/mermaid";
import { resolvePageLinkAction } from "./linkInterception";
import type { DirEntry, PageDirResponse, PageResponse } from "./types";
import { isDirResponse } from "./types";
import "./style.css";

function Skeleton() {
  return (
    <div className="page-skeleton" aria-busy="true" aria-live="polite">
      <div className="page-skeleton-line page-skeleton-title" />
      <div className="page-skeleton-line" />
      <div className="page-skeleton-line" />
      <div className="page-skeleton-line page-skeleton-short" />
    </div>
  );
}

function NotFound({ relpath }: { relpath: string }) {
  return (
    <div className="page-not-found">
      <h1>Not found</h1>
      <p>
        <code>{relpath}</code> does not exist in this realm.
      </p>
    </div>
  );
}

function DirListing({ dir }: { dir: PageDirResponse }) {
  return (
    <div className="page-dir-listing">
      <h1>{dir.relpath || "/"}</h1>
      <ul>
        {dir.entries.map((entry: DirEntry) => (
          <li key={entry.relpath}>
            <a href={`/page/${entry.relpath}`} className="wikilink">
              {entry.name}
              {entry.folder ? "/" : ""}
            </a>
          </li>
        ))}
      </ul>
    </div>
  );
}

export function Page() {
  const params = useParams();
  const relpath = params["*"] ?? "";
  // The "/" index route carries no relpath — the pre-cutover shell resolved
  // landing via computeLandingURL (serve.go), a value never exposed as an
  // /api/* field (no checkpoint owns it). README.md is that function's own
  // repo-scope default, so it's the least-surprising fetch target here;
  // realm scope's wiki/index.md landing isn't reachable from "/" until a
  // later checkpoint surfaces it (falls through to the 404 view instead —
  // no worse than the pre-CP6 stub route it replaces).
  const fetchPath = relpath || "README.md";
  const navigate = useNavigate();
  const { get } = useApi();
  const { data, loading, failure } = get<PageResponse | PageDirResponse>(`/page/${fetchPath}`);
  const bodyRef = useRef<HTMLDivElement>(null);

  const resolvedRelpath = data && !isDirResponse(data) ? data.relpath : null;

  useEffect(() => {
    events.emit("page.resolved", { relpath: resolvedRelpath });
  }, [resolvedRelpath]);

  useEffect(() => {
    if (!data || isDirResponse(data) || !data.hasMermaid || !bodyRef.current) return;
    void mountMermaid(bodyRef.current);
  }, [data]);

  if (loading && !data) return <Skeleton />;

  if (failure || !data) return <NotFound relpath={relpath} />;

  if (isDirResponse(data)) return <DirListing dir={data} />;

  function handleClick(e: React.MouseEvent<HTMLDivElement>) {
    const action = resolvePageLinkAction(e.target);
    if (action.kind === "navigate") {
      e.preventDefault();
      navigate(`/page/${action.relpath}`);
    } else if (action.kind === "code") {
      e.preventDefault();
      openFile(action.path, action.line);
    }
  }

  return (
    <div className="page-view" data-route="page">
      <nav className="page-breadcrumb" aria-label="Breadcrumb">
        {data.breadcrumb.map((seg, i) => (
          <span key={`${seg.label}:${i}`}>
            {seg.path ? <a href={`/page/${seg.path}`}>{seg.label}</a> : seg.label}
            {i < data.breadcrumb.length - 1 ? " / " : ""}
          </span>
        ))}
      </nav>
      <div
        ref={bodyRef}
        className="page-body"
        onClick={handleClick}
        // eslint-disable-next-line react/no-danger -- server-rendered markdown, same trust domain as the pre-cutover htmx fragments
        dangerouslySetInnerHTML={{ __html: data.html }}
      />
    </div>
  );
}
