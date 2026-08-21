// PlansView — the Plans list: one row per slug, aggregated across every
// worktree of a repo. No checkout control lives here — a row already spans
// every checkout, which is what makes it an aggregate (see
// docs/design/serve-plans-page.md "The list is an aggregate; version choice
// belongs to the file"). Version choice belongs to the opened slug (CP13).
import { useEffect, useRef, useState } from "react";
import { fetchPlanMembers, fetchPlans, type PlanRow, type PlansMember, bundleLocalPath, formatDate } from "../../utils/plansApi";
import { filterPlanRows } from "../search/searchItems";
import { usePlansScope } from "./usePlansScope";
import "./style.css";

function isEditableTarget(el: Element | null): boolean {
  if (!el) return false;
  const tag = el.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || (el instanceof HTMLElement && el.isContentEditable);
}

function memberLabel(m: PlansMember): string {
  return m.prefix || "(local)";
}

// Chips name only the parts a row actually carries — never invented from
// what a chip *could* mean. Design/spec come from the committed docs half;
// the rest come from whichever worktree's bundle holds them.
export function chipsFor(row: PlanRow): string[] {
  const chips: string[] = [];
  const docs = row.docs ?? [];
  const bundles = row.bundles ?? [];
  if (docs.some((d) => d.path.endsWith(`docs/design/${row.slug}.md`))) chips.push("design");
  if (docs.some((d) => d.path.endsWith(`docs/spec/${row.slug}.md`))) chips.push("spec");

  const files = bundles.flatMap((b) => b.files ?? []);
  if (files.some((f) => bundleLocalPath(f.relpath) === "BRIEF.md")) chips.push("brief");
  if (files.some((f) => bundleLocalPath(f.relpath) === "STATE.md")) chips.push("state");
  if (files.some((f) => bundleLocalPath(f.relpath) === "FOLLOWUPS.md")) chips.push("followups");
  if (files.some((f) => bundleLocalPath(f.relpath).startsWith("findings/"))) chips.push("findings");
  if (files.some((f) => bundleLocalPath(f.relpath) === "options.html")) chips.push("options");
  return chips;
}

function PlanRowView({ row, onOpen }: { row: PlanRow; onOpen: (slug: string) => void }) {
  const chips = chipsFor(row);
  const updated = formatDate(row.updatedAt);
  return (
    <li className="plans-row" onClick={() => onOpen(row.slug)}>
      <div className="plans-row-head">
        <span className="plans-row-title">{row.title || row.slug}</span>
        <span className="plans-row-slug">{row.slug}</span>
        {row.dotCount > 0 ? (
          <span className="plans-row-dots" aria-label={`${row.dotCount} versions`}>
            {Array.from({ length: row.dotCount }, (_, i) => (
              <span
                key={i}
                className="plans-dot"
                data-filled={row.dotMerged && i === 0 ? "" : undefined}
              />
            ))}
          </span>
        ) : null}
      </div>
      {row.description ? <div className="plans-row-desc">{row.description}</div> : null}
      {updated ? <div className="plans-row-updated">updated {updated}</div> : null}
      {chips.length > 0 ? (
        <div className="plans-row-chips">
          {chips.map((c) => (
            <span key={c} className="plans-chip">
              {c}
            </span>
          ))}
        </div>
      ) : null}
    </li>
  );
}

export function PlansView() {
  const { member, openSlug, setMember } = usePlansScope();
  const [members, setMembers] = useState<PlansMember[]>([]);
  const [rows, setRows] = useState<PlanRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [query, setQuery] = useState("");
  const filterRef = useRef<HTMLInputElement>(null);

  // ⌘F / Ctrl+F focuses the filter while this route is mounted, unless focus
  // already sits in a text field — the browser's own find is left alone
  // everywhere else. Bound on window and torn down on unmount so navigating
  // away releases the capture.
  useEffect(() => {
    const onKeyDown = (e: KeyboardEvent) => {
      if (!(e.metaKey || e.ctrlKey) || e.key.toLowerCase() !== "f") return;
      if (isEditableTarget(document.activeElement)) return;
      e.preventDefault();
      filterRef.current?.focus();
      filterRef.current?.select();
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  function onFilterKeyDown(e: React.KeyboardEvent<HTMLInputElement>) {
    if (e.key === "Escape") {
      setQuery("");
      filterRef.current?.blur();
    }
  }

  useEffect(() => {
    let cancelled = false;
    void fetchPlanMembers().then((m) => {
      if (!cancelled) setMembers(m);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    void fetchPlans(member).then((r) => {
      if (!cancelled) {
        setRows(r);
        setLoading(false);
      }
    });
    return () => {
      cancelled = true;
    };
  }, [member]);

  const filteredRows = filterPlanRows(rows, query);

  return (
    <div className="page-content-inner plans-route" data-route="plans">
      <div className="plans-toolbar">
        <div className="plans-title-line">
          <h1>Plans</h1>
          {query ? (
            <span className="plans-filter-count">
              {filteredRows.length} of {rows.length}
            </span>
          ) : null}
          {members.length > 1 ? (
            <select
              className="plans-member-select"
              aria-label="Repo"
              value={member ?? ""}
              onChange={(e) => setMember(e.target.value)}
            >
              {members.map((m) => (
                <option key={m.key} value={m.key}>
                  {memberLabel(m)}
                </option>
              ))}
            </select>
          ) : null}
        </div>
        <input
          ref={filterRef}
          type="search"
          className="plans-filter-input"
          placeholder="filter plans"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onFilterKeyDown}
        />
      </div>

      {loading ? (
        <p className="plans-loading">Loading…</p>
      ) : rows.length === 0 ? (
        <p className="plans-empty">No plans found.</p>
      ) : filteredRows.length === 0 ? (
        <p className="plans-empty">No plans match "{query}".</p>
      ) : (
        <ul className="plans-list">
          {filteredRows.map((row) => (
            <PlanRowView key={row.slug} row={row} onOpen={openSlug} />
          ))}
        </ul>
      )}
    </div>
  );
}
