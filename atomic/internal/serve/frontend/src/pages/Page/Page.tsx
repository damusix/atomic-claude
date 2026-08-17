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
import { faFolder } from "@fortawesome/free-solid-svg-icons";
import { openFile } from "../../components/code-modal/store";
import { FaGlyph, FileIcon } from "../../components/ui";
import { shouldRefetchPage } from "../../hooks/useLiveReload";
import { useApi } from "../../utils/api";
import { emitPageHeadings, events, type PageHeading } from "../../utils/events";
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
        {dir.entries.map((entry: DirEntry) => {
          // The title is what the document calls itself; the addressable name
          // is how it is linked to. Both are shown — unless they are the same
          // string, which is the common case for an untitled file and would
          // otherwise print twice.
          const title = entry.title || entry.name;
          const addressable = entry.folder ? `${entry.name}/` : (entry.filename ?? entry.name);

          return (
            <li key={entry.relpath} className="dir-entry">
              <a href={`/page/${entry.relpath}`} className="dir-entry-link">
                {entry.folder ? (
                  <FaGlyph icon={faFolder} size={12} className="dir-entry-icon" />
                ) : (
                  <FileIcon relpath={entry.filename ?? entry.relpath} className="dir-entry-icon" />
                )}
                <span className="dir-entry-text">
                  <span className="dir-entry-head">
                    <span className="dir-entry-title">{title}</span>
                    {addressable === title ? null : (
                      <span className="dir-entry-name">{addressable}</span>
                    )}
                    {/* A folder that opens a page behaves differently from one
                        that opens another listing. */}
                    {entry.folder ? (
                      <span className="dir-entry-badge">{entry.index ? "index" : "folder"}</span>
                    ) : null}
                  </span>
                  {entry.summary ? (
                    <span className="dir-entry-summary">{entry.summary}</span>
                  ) : null}
                </span>
              </a>
            </li>
          );
        })}
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

  // Wide tables get their own scroll container rather than widening the
  // column or wrapping cells into unreadable slivers. goldmark emits a bare
  // <table>, so the wrapper is added here — the same post-injection pass
  // mermaid already uses.
  useEffect(() => {
    const body = bodyRef.current;
    if (!body) return;
    for (const table of body.querySelectorAll("table")) {
      if (table.parentElement?.classList.contains("table-scroll")) continue;
      const scroller = document.createElement("div");
      scroller.className = "table-scroll";
      table.replaceWith(scroller);
      scroller.appendChild(table);
    }
  }, [data]);

  // Publish the on-page contents. Read from the injected DOM rather than
  // parsed from markdown: goldmark already assigned the anchor ids, and
  // reading them back is what guarantees the rail's links match the ids that
  // actually exist on the page.
  useEffect(() => {
    const body = bodyRef.current;
    if (!body) {
      emitPageHeadings([]);
      return;
    }
    const headings: PageHeading[] = [];
    for (const el of body.querySelectorAll("h1, h2, h3, h4")) {
      if (!el.id) continue;
      headings.push({
        id: el.id,
        text: el.textContent?.trim() ?? "",
        level: Number(el.tagName.slice(1)),
      });
    }
    emitPageHeadings(headings);
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
    // No in-page breadcrumb: the header carries one already, and two
    // breadcrumbs for one page is one too many — especially when only the
    // header's is always visible.
    <div className="page-content-inner page-view" data-route="page">
      <div
        ref={bodyRef}
        // md-content carries the whole editorial typography set in the
        // carried app.css — serif heading scale, prose measure, links,
        // blockquotes, lists, tables, mermaid sizing. The React cutover
        // rendered the body without it, so none of it applied.
        className="page-body md-content"
        onClick={handleClick}
        // eslint-disable-next-line react/no-danger -- server-rendered markdown, same trust domain as the pre-cutover htmx fragments
        dangerouslySetInnerHTML={{ __html: data.html }}
      />
    </div>
  );
}
