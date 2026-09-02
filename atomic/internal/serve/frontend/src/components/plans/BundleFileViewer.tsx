// BundleFileViewer — renders a bundle file by kind. `html` and `file` are
// sourced by URL (`src=`/`href=`) rather than fetched into the React tree,
// keeping the bytes out of React so an atomic-visual-options HTML fixture —
// deliberately offline, inline CSS, not written against the app's own
// stylesheet — never touches it. The `html` iframe runs its own scripts
// (`sandbox="allow-scripts"`); the frame's opaque origin keeps them from the
// app's cookies and storage, and origin_guard.go on the server is what
// stops a script running there from reaching the write routes.
import { useEffect, useLayoutEffect, useMemo, useState } from "react";
import { fetchPlanPage } from "../../utils/plansApi";
import type { PageResponse } from "../../pages/Page/types";
import "./style.css";

function rawUrl(checkoutId: string, relpath: string): string {
  const params = new URLSearchParams({ worktree: checkoutId, path: relpath, raw: "1" });
  return `${window.location.origin}/api/plans/page?${params.toString()}`;
}

function filename(relpath: string): string {
  return relpath.split("/").pop() ?? relpath;
}

export function BundleFileViewer({
  checkoutId,
  relpath,
  kind,
}: {
  checkoutId: string;
  relpath: string;
  kind: "markdown" | "html" | "file";
}) {
  const [page, setPage] = useState<PageResponse | null>(null);

  useEffect(() => {
    if (kind !== "markdown") return;
    setPage(null);
    let cancelled = false;
    void fetchPlanPage(checkoutId, relpath).then((res) => {
      if (!cancelled) setPage(res);
    });
    return () => {
      cancelled = true;
    };
  }, [kind, checkoutId, relpath]);

  const pageHtml = useMemo(() => ({ __html: page?.html ?? "" }), [page]);

  if (kind === "html") {
    return <HtmlWindow src={rawUrl(checkoutId, relpath)} relpath={relpath} />;
  }

  if (kind === "file") {
    return (
      <div className="bundle-file-download">
        <a href={rawUrl(checkoutId, relpath)} download>
          {filename(relpath)}
        </a>
      </div>
    );
  }

  if (!page) {
    return <div className="page-content-inner plans-slug-loading">Loading…</div>;
  }

  return (
    <div
      className="page-body md-content"
      // eslint-disable-next-line react/no-danger -- server-rendered markdown, same trust domain as pages/Page
      dangerouslySetInnerHTML={pageHtml}
    />
  );
}

// A self-contained HTML artifact is a rendered page, so it gets the same
// window chrome a code fence does — the dots bar says "this is a thing being
// shown to you, not text of ours" — and fills the pane rather than the prose
// measure. The body class is what app.css keys the pane rules on, the same
// mechanism Graph mode uses; a layout effect so the padding is gone before
// first paint.
function HtmlWindow({ src, relpath }: { src: string; relpath: string }) {
  useLayoutEffect(() => {
    document.body.classList.add("mode-plans-frame");
    return () => document.body.classList.remove("mode-plans-frame");
  }, []);
  return (
    <figure className="code-window bundle-file-window">
      <figcaption className="code-window-bar">
        <span className="code-window-dots" aria-hidden="true" />
        <span className="code-window-lang">{filename(relpath)}</span>
      </figcaption>
      <iframe className="bundle-file-frame" sandbox="allow-scripts" src={src} title={relpath} />
    </figure>
  );
}
