// SlugView — opened slug (/plans/:slug/*): middle pane renders the active
// file (a committed doc or a bundle file). The right rail (PlansRail, mounted
// separately in Shell's aside) carries the version picker, the bundle's
// parts, and the active file's headings — see docs/design/serve-plans-page.md
// "Reading a slug". This component owns the sticky `?at=` selection and its
// yield: PlansRail computes the same resolution read-only from resolve.ts,
// but only SlugView writes the URL, so the two can't fight each other.
import { useEffect, useMemo, useRef, useState } from "react";
import { fetchPlanPage, fetchPlans, type PlanRow } from "../../utils/plansApi";
import { BundleFileViewer } from "./BundleFileViewer";
import type { PageResponse } from "../../pages/Page/types";
import { emitPageHeadings, type PageHeading } from "../../utils/events";
import { mountMermaid } from "../../utils/mermaid";
import { findDoc, resolveBundleFile, resolveDocVersion } from "./resolve";
import { usePlansScope } from "./usePlansScope";
import "./style.css";

export function SlugView() {
  const { slug = "", relpath, at, member, openFile, setAt } = usePlansScope();
  const activeRelpath = relpath ?? "";

  const [row, setRow] = useState<PlanRow | null>(null);
  const [rowLoading, setRowLoading] = useState(true);
  const [page, setPage] = useState<PageResponse | null>(null);
  const bodyRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let cancelled = false;
    setRowLoading(true);
    void fetchPlans(member).then((rows) => {
      if (!cancelled) {
        setRow(rows.find((r) => r.slug === slug) ?? null);
        setRowLoading(false);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [slug, member]);

  const doc = row ? findDoc(row, activeRelpath) : undefined;
  const docResolution = doc ? resolveDocVersion(doc, at) : null;
  const bundleResolution = row && !doc ? resolveBundleFile(row, activeRelpath, at) : null;

  // No file segment yet (a row was just opened): land on the spec doc, else
  // the design doc, else whatever the row has.
  useEffect(() => {
    if (!row || activeRelpath) return;
    const specDoc = row.docs?.find((d) => d.path.endsWith(`docs/spec/${slug}.md`));
    const designDoc = row.docs?.find((d) => d.path.endsWith(`docs/design/${slug}.md`));
    const target = specDoc ?? designDoc ?? row.docs?.[0];
    if (target) openFile(target.path, { replace: true });
  }, [row, activeRelpath, slug, openFile]);

  // The yield: navigation always wins. When the resolved checkout doesn't
  // match the sticky selection, the URL moves to say where the reader is —
  // never the other way around.
  const resolvedBranch = docResolution?.checkout.branch ?? bundleResolution?.bundle.branch;
  useEffect(() => {
    if (!resolvedBranch || resolvedBranch === at) return;
    setAt(resolvedBranch, { replace: true });
  }, [resolvedBranch, at, setAt]);

  const fetchWorktreeId = docResolution
    ? docResolution.checkout.id
    : bundleResolution && bundleResolution.file.kind === "markdown"
      ? bundleResolution.bundle.worktreeId
      : undefined;

  useEffect(() => {
    if (!fetchWorktreeId) {
      setPage(null);
      return;
    }
    let cancelled = false;
    void fetchPlanPage(fetchWorktreeId, activeRelpath).then((res) => {
      if (!cancelled) setPage(res);
    });
    return () => {
      cancelled = true;
    };
  }, [fetchWorktreeId, activeRelpath]);

  const pageHtmlString = page?.html ?? "";
  const pageHtml = useMemo(() => ({ __html: pageHtmlString }), [pageHtmlString]);

  useEffect(() => {
    if (!page?.hasMermaid || !bodyRef.current) return;
    void mountMermaid(bodyRef.current);
  }, [page]);

  useEffect(() => {
    const body = bodyRef.current;
    if (!body) {
      emitPageHeadings([]);
      return;
    }
    const headings: PageHeading[] = [];
    for (const el of body.querySelectorAll("h1, h2, h3, h4")) {
      if (!el.id) continue;
      headings.push({ id: el.id, text: el.textContent?.trim() ?? "", level: Number(el.tagName.slice(1)) });
    }
    emitPageHeadings(headings);
  }, [page]);

  if (rowLoading) {
    return <div className="page-content-inner plans-slug-loading">Loading…</div>;
  }

  if (!row) {
    return (
      <div className="page-content-inner plans-slug-not-found">
        <h1>Not found</h1>
        <p>
          No plan named <code>{slug}</code> in this scope.
        </p>
      </div>
    );
  }

  if (bundleResolution && bundleResolution.file.kind === "html") {
    return (
      <BundleFileViewer
        checkoutId={bundleResolution.bundle.worktreeId}
        relpath={bundleResolution.file.relpath}
        kind="html"
      />
    );
  }

  if (bundleResolution && bundleResolution.file.kind === "file") {
    return (
      <div className="page-content-inner" data-route="plans-slug">
        <BundleFileViewer
          checkoutId={bundleResolution.bundle.worktreeId}
          relpath={bundleResolution.file.relpath}
          kind="file"
        />
      </div>
    );
  }

  if (!docResolution && !bundleResolution) {
    if (!activeRelpath) return <div className="page-content-inner plans-slug-loading">Loading…</div>;
    return (
      <div className="page-content-inner plans-slug-not-found">
        <h1>Not found</h1>
        <p>
          <code>{activeRelpath}</code> is not part of this slug.
        </p>
      </div>
    );
  }

  if (!page) {
    return <div className="page-content-inner plans-slug-loading">Loading…</div>;
  }

  return (
    <div className="page-content-inner page-view" data-route="plans-slug">
      <div
        ref={bodyRef}
        className="page-body md-content"
        // eslint-disable-next-line react/no-danger -- server-rendered markdown, same trust domain as pages/Page
        dangerouslySetInnerHTML={pageHtml}
      />
    </div>
  );
}
