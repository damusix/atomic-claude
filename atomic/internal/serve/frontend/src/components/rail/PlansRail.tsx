// PlansRail — right rail for an opened slug (/plans/:slug/*): Bundle tab
// carries the version picker, the row's docs + bundle files (what the ROW
// aggregates, never what the current selection contains — no entry is ever
// disabled), and the active file's headings via the shared Contents
// component. Mounted in Shell's aside, outside SlugView's Outlet subtree —
// same reason components/rail/Rail.tsx fetches its own /api/rail copy
// rather than reading Page's state: it re-derives slug/relpath from the
// route itself and computes the same resolution SlugView does (resolve.ts),
// but never writes `?at=` — SlugView owns that write so the two can't race.
import { useEffect, useState } from "react";
import { fetchPlans, type PlanRow, bundleLocalPath } from "../../utils/plansApi";
import { useCurrentMember } from "../../utils/memberStore";
import { Contents } from "./Contents";
import { findDoc, resolveBundleFile, resolveDocVersion } from "../plans/resolve";
import { VersionPicker } from "../plans/VersionPicker";
import { usePlansScope } from "../plans/usePlansScope";
import "../plans/style.css";

// A design doc and its spec share the slug's filename (docs/design/<slug>.md,
// docs/spec/<slug>.md) — the real basename collides, so the nav label names
// the half of the split instead (BRIEF: "design.md, spec.md").
function docLabel(path: string): string {
  if (path.includes("/design/")) return "design.md";
  if (path.includes("/spec/")) return "spec.md";
  const i = path.lastIndexOf("/");
  return i === -1 ? path : path.slice(i + 1);
}

export function PlansRail() {
  const { slug = "", relpath, at, openFile, setAt } = usePlansScope();
  const { member, ready } = useCurrentMember();
  const activeRelpath = relpath ?? "";

  const [row, setRow] = useState<PlanRow | null>(null);

  useEffect(() => {
    if (!ready) return;
    let cancelled = false;
    void fetchPlans(member).then((rows) => {
      if (!cancelled) setRow(rows.find((r) => r.slug === slug) ?? null);
    });
    return () => {
      cancelled = true;
    };
  }, [slug, member, ready]);

  if (!row) return null;

  const doc = findDoc(row, activeRelpath);
  const docResolution = doc ? resolveDocVersion(doc, at) : null;
  const bundleResolution = !doc ? resolveBundleFile(row, activeRelpath, at) : null;

  const docEntries = (row.docs ?? []).map((d) => ({ key: d.path, label: docLabel(d.path), relpath: d.path }));
  const bundles = row.bundles ?? [];

  return (
    <div className="rail-tabs plans-rail-tabs">
      <div className="rail-panel">
        {docResolution && docResolution.doc.versions.length > 1 ? (
          <VersionPicker
            versions={docResolution.doc.versions}
            active={docResolution.version}
            onSelect={(version) => {
              const checkout = version.checkouts.find((c) => c.branch === version.label) ?? version.checkouts[0];
              if (!checkout) return;
              setAt(checkout.branch);
            }}
          />
        ) : bundleResolution ? (
          <>
            <div className="rail-slot-label">Version</div>
            <div className="vpick-static">{bundleResolution.bundle.branch}</div>
          </>
        ) : null}

        <div className="rail-slot-label">Bundle</div>
        <div className="bnav">
          {docEntries.map((entry) => (
            <div
              key={entry.key}
              className={entry.relpath === activeRelpath ? "on" : undefined}
              role="button"
              tabIndex={0}
              onClick={() => openFile(entry.relpath)}
              onKeyDown={(e) => {
                if (e.key === "Enter" || e.key === " ") openFile(entry.relpath);
              }}
            >
              {entry.label}
            </div>
          ))}
          {bundles.map((bundle) => (
            <div key={bundle.worktreeId} className="bnav-group">
              {bundles.length > 1 && <div className="bnav-group-header">{bundle.branch}</div>}
              {bundle.files.map((f) => (
                <div
                  key={f.relpath}
                  className={
                    f.relpath === activeRelpath && bundle.worktreeId === bundleResolution?.bundle.worktreeId
                      ? "on"
                      : undefined
                  }
                  role="button"
                  tabIndex={0}
                  onClick={() => openFile(f.relpath, { at: bundle.branch })}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" || e.key === " ") openFile(f.relpath, { at: bundle.branch });
                  }}
                >
                  {bundleLocalPath(f.relpath)}
                </div>
              ))}
            </div>
          ))}
        </div>

        <div className="rail-slot-label">Contents</div>
        <Contents />
      </div>
    </div>
  );
}
