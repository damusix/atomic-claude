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
import { shouldRefetchPage } from "../../hooks/useLiveReload";
import { useApi } from "../../utils/api";
import { events } from "../../utils/events";
import { mountMermaid } from "../../utils/mermaid";
import { resolvePageLinkAction } from "./linkInterception";
import type { DirEntry, PageDirResponse, PageResponse } from "./types";
import { isDirResponse } from "./types";
import "./style.css";

function Skeleton() {
  return (
    <div className="page-content-inner page-skeleton" aria-busy="true" aria-live="polite">
      <div className="page-skeleton-line page-skeleton-title" />
      <div className="page-skeleton-line" />
      <div className="page-skeleton-line" />
      <div className="page-skeleton-line page-skeleton-short" />
    </div>
  );
}

function NotFound({ relpath }: { relpath: string }) {
  return (
    <div className="page-content-inner page-not-found">
      <h1>Not found</h1>
      <p>
        <code>{relpath}</code> does not exist in this realm.
      </p>
    </div>
  );
}

function DirListing({ dir }: { dir: PageDirResponse }) {
  return (
    <div className="page-content-inner page-dir-listing">
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
  // The "/" index route carries no relpath — /api/page/ with an empty relpath
  // resolves the scope's landing server-side (realm → wiki/index.md, repo →
  // README.md) and returns the resolved relpath in the response.
  const navigate = useNavigate();
  const { get } = useApi();
  const { data, loading, failure, refetch } = get<PageResponse | PageDirResponse>(`/page/${relpath}`);
  const bodyRef = useRef<HTMLDivElement>(null);

  const resolvedRelpath = data && !isDirResponse(data) ? data.relpath : null;

  useEffect(() => {
    events.emit("page.resolved", { relpath: resolvedRelpath });
  }, [resolvedRelpath]);

  // Live-reload reconcile (spec Flow): refetch this page only when it's the
  // one that changed (or the server's diff exceeded its cap and omitted the
  // list — shouldRefetchPage treats that as "everything changed"). Re-fetch
  // re-renders the same route in place — no pane swap, so scroll position is
  // preserved as a natural property of the update rather than anything this
  // effect has to manage itself.
  useEffect(() => {
    return events.on("realm.changed", ({ changed }) => {
      if (shouldRefetchPage(resolvedRelpath, changed)) refetch();
    });
  }, [resolvedRelpath, refetch]);

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
    <div className="page-content-inner page-view" data-route="page">
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
