// SchemaView — SQL schema view (tables/views/columns/FK sources/writers),
// mounted at the /code/schema route (pages/Schema). Reuses the
// /code/graph/members member picker convention pages/Graph/Graph.tsx already
// established (carried, non-/api/* endpoint — utils/graphEngineAdapter's
// fetchGraphMembers/resolveMember), since /api/code/schema itself carries no
// member list — only the selected member's tables. Node/column/FK/writer
// names open the code modal via the store.ts openNode seam, mirroring
// codeexplorer.go's renderTableSchema drill-down links.
import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useSearchParams } from "react-router";
import { openNode } from "../code-modal/store";
import type { ApiCodeNode } from "../code-modal/types";
import { useApi } from "../../utils/api";
import { emitSchemaIndex } from "../../utils/events";
import { fetchGraphMembers, resolveMember, type GraphMember } from "../../utils/graphEngineAdapter";
import { columnViews, constraintLabel, keyViews } from "./columns";
import { groupTables, tableMatches, type SchemaGroup, type SchemaSection } from "./grouping";
import { SchemaToolbar } from "./SchemaToolbar";
import { useMasonry } from "./useMasonry";
import type { ApiCodeSchemaResponse, ApiRoutineSchema, ApiTableSchema } from "./types";

/** Same case-insensitive substring rule the table filter uses. */
function nodeMatches(name: string, query: string): boolean {
  return !query || name.toLowerCase().includes(query.toLowerCase());
}
import "./style.css";

function memberLabel(m: GraphMember): string {
  return (m.prefix || "(local)") + (m.indexed ? "" : " — not indexed");
}

function NodeLink({ node, member }: { node: ApiCodeNode; member: string }) {
  return (
    <button
      type="button"
      className="code-schema-link code-node-link"
      onClick={() => openNode(node.id, member, { title: node.name, file: node.filePath, line: node.startLine })}
    >
      {node.name}
    </button>
  );
}

/** A hot table is read by dozens of routines. Listing all of them makes one
    card taller than the screen, which in a grid of cards sets the height of
    everything beside it — so the list is capped and opens on request. */
const RELATION_LIMIT = 6;

/** Chip row for a related-node list (FK sources, writers). */
function RelationRow({
  label,
  nodes,
  member,
}: {
  label: string;
  nodes: ApiCodeNode[];
  member: string;
}) {
  const [expanded, setExpanded] = useState(false);
  if (nodes.length === 0) return null;

  const capped = nodes.length > RELATION_LIMIT;
  const shown = capped && !expanded ? nodes.slice(0, RELATION_LIMIT) : nodes;

  return (
    <div className="code-schema-relation">
      <span className="code-schema-relation-label">{label}</span>
      <span className="code-schema-relation-nodes">
        {shown.map((n) => (
          <NodeLink key={n.id} node={n} member={member} />
        ))}
        {capped ? (
          <button
            type="button"
            className="code-schema-relation-more"
            onClick={() => setExpanded((v) => !v)}
          >
            {expanded ? "show fewer" : `+${nodes.length - RELATION_LIMIT} more`}
          </button>
        ) : null}
      </span>
    </div>
  );
}

function TableCard({ table, member }: { table: ApiTableSchema; member: string }) {
  // The API returns columns, constraints and indexes in one `columns` array,
  // so a table renders as an undifferentiated list where pk_/fk_/idx_ entries
  // sit among the actual columns. Splitting by node kind is what makes the
  // shape of a table readable at a glance.
  const columns = columnViews(table.columns);
  const keys = keyViews(table.columns);
  const constraints = keys.filter((k) => k.kind !== "index");
  const indexes = keys.filter((k) => k.kind === "index");

  return (
    <div className="code-schema-table" id={`tbl-${table.node.id}`} data-kind={table.node.kind}>
      <div className="code-schema-table-head">
        <h4 className="code-schema-table-name">
          <span className="code-schema-table-kind">{table.node.kind}</span>
          <NodeLink node={table.node} member={member} />
        </h4>
        <span className="code-schema-table-counts">
          {columns.length} {columns.length === 1 ? "column" : "columns"}
          {constraints.length > 0 ? ` · ${constraints.length} keys` : ""}
          {indexes.length > 0 ? ` · ${indexes.length} idx` : ""}
        </span>
      </div>

      {/* A two-column name/type grid rather than a bare name list: the type is
          what identifies these as columns at a glance, and it is also where a
          project's own domains (tg_no, tg_email) become visible. */}
      {columns.length > 0 ? (
        <ul className="code-schema-columns">
          {columns.map((col) => (
            <li key={col.node.id} className="code-schema-column">
              <span className="code-schema-column-name">{col.name}</span>
              <span className="code-schema-column-flags">
                {col.pk ? <span className="code-schema-flag" data-flag="pk">PK</span> : null}
                {col.fk ? <span className="code-schema-flag" data-flag="fk">FK</span> : null}
                {col.unique && !col.pk ? (
                  <span className="code-schema-flag" data-flag="unique">U</span>
                ) : null}
              </span>
              <code className="code-schema-column-type">{col.dataType || "—"}</code>
            </li>
          ))}
        </ul>
      ) : null}

      {/* Constraint names are long and near-identical (ck_app_setting_kind_is_
          string_bool_number_array_or_obj); the kind in front is what makes the
          row scannable. */}
      {keys.length > 0 ? (
        <div className="code-schema-keys">
          {keys.map((k) => (
            <span key={k.node.id} className="code-schema-key" data-kind={k.kind} title={k.name}>
              <span className="code-schema-key-kind">{constraintLabel(k.kind)}</span>
              <span className="code-schema-key-name">
                {k.columns.length ? k.columns.join(", ") : k.name}
              </span>
            </span>
          ))}
        </div>
      ) : null}

      <RelationRow label="Read by" nodes={table.fkSources} member={member} />
      <RelationRow label="Written by" nodes={table.writers} member={member} />
    </div>
  );
}

/** A stored routine and the tables it touches. A routine list without those
    is a list of names, which is the problem the table cards just stopped
    having. */
function RoutineCard({ routine, member }: { routine: ApiRoutineSchema; member: string }) {
  return (
    <div className="code-schema-routine" id={`rtn-${routine.node.id}`}>
      <h4 className="code-schema-table-name">
        <span className="code-schema-table-kind">{routine.node.kind}</span>
        <NodeLink node={routine.node} member={member} />
      </h4>
      <RelationRow label="Reads" nodes={routine.reads} member={member} />
      <RelationRow label="Writes" nodes={routine.writes} member={member} />
      {routine.reads.length === 0 && routine.writes.length === 0 ? (
        <p className="code-schema-routine-empty">no table access found</p>
      ) : null}
    </div>
  );
}

/** The objects declared in one .sql file: a heading and its cards, both
 *  flowing in the section's shared column flow (see .code-schema-groups).
 *
 *  The group deliberately does NOT get a box of its own. Cards range from a
 *  three-row lookup table to a twelve-column queue table with two relation
 *  lists, and any per-file box has to be as tall as its tallest card, so a
 *  file declaring one tall table and two short ones left an L-shaped void
 *  the size of the tall card beside them. Nothing can fill that void except
 *  cards from another file, which is exactly what a box prevents.
 *
 *  Flowing every file through one set of columns lets the next file's cards
 *  pack under this one's short ones. The heading still sits directly above
 *  the cards it names, and keeps the id the rail scrolls to.
 */
function FileGroup({ group, member }: { group: SchemaGroup; member: string }) {
  // A fragment, not a wrapper: the heading and the cards each have to be a
  // grid item in their own right for the masonry to place them. Wrapped, the
  // whole file would be one item, and a 25-table file would be one column-wide
  // block as tall as the section. The id rides on the heading so the rail's
  // jump still lands on the file.
  return (
    <>
      <h4 className="code-schema-group-title" id={group.id}>
        {group.title}
        <span className="code-schema-group-count">{group.tables.length}</span>
      </h4>
      {group.tables.map((t) => (
        <TableCard key={t.node.id} table={t} member={member} />
      ))}
    </>
  );
}

/** A masonry container. Children lay out left to right, each reserving its own
    height rather than its row's. See useMasonry for the measuring. */
function Masonry({
  className,
  deps,
  children,
}: {
  className: string;
  deps: unknown[];
  children: React.ReactNode;
}) {
  const ref = useRef<HTMLDivElement>(null);
  useMasonry(ref, deps);
  return (
    <div className={className} ref={ref}>
      {children}
    </div>
  );
}

/** One column flow per directory, unbounded.
 *
 *  Bounding it into runs of N cards was an attempt to keep a column near a
 *  screen tall, since one flow over 108 tables gives five columns of about
 *  8000px each. It cost more than it bought: a run cannot split a card, so
 *  every boundary left the short columns standing empty for up to a card's
 *  height — ~315px a boundary, whatever N was — and the first card of the
 *  next run butted straight up against the last card of the previous one.
 *  Tall columns are a browsing inconvenience; a page pocked with holes is a
 *  broken-looking page. */
function DirSection({ section, member }: { section: SchemaSection; member: string }) {
  return (
    <section className="code-schema-section" id={section.id}>
      <h3 className="code-schema-section-title">
        {section.title}
        <span className="code-schema-group-count">{section.count}</span>
      </h3>
      <Masonry className="code-schema-groups" deps={[section]}>
        {section.groups.map((g) => (
          <FileGroup key={g.id} group={g} member={member} />
        ))}
      </Masonry>
    </section>
  );
}

export function SchemaView() {
  // Member lives in the URL, as it does on the graph route — otherwise the
  // page always opens on whichever member sorts first and a link to a
  // specific one cannot be shared or bookmarked.
  const [searchParams, setSearchParams] = useSearchParams();
  const memberParam = searchParams.get("member") ?? "";
  const [members, setMembers] = useState<GraphMember[]>([]);
  const [member, setMember] = useState(memberParam);
  // Fetching /code/schema before the member list resolves 500s at a realm
  // root (no realm-level index) — hold the data fetch until membership is
  // known, then fetch per-member (or memberless in bare-repo scope).
  const [membersResolved, setMembersResolved] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void fetchGraphMembers().then((fetched) => {
      if (cancelled) return;
      setMembers(fetched);
      setMember((prev) => resolveMember(fetched, prev || memberParam));
      setMembersResolved(true);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  function selectMember(next: string) {
    setMember(next);
    setSearchParams((prev) => {
      const params = new URLSearchParams(prev);
      params.set("member", next);
      return params;
    });
  }

  const path = member ? `/code/schema?member=${encodeURIComponent(member)}` : "/code/schema";
  return membersResolved ? (
    <SchemaData key={path} path={path} members={members} member={member} setMember={selectMember} />
  ) : (
    <div className="page-content-inner code-schema" data-route="schema">
      <h2 className="code-schema-title">SQL Schema</h2>
      <p className="code-schema-loading">Loading…</p>
    </div>
  );
}

function SchemaData({
  path,
  members,
  member,
  setMember,
}: {
  path: string;
  members: GraphMember[];
  member: string;
  setMember: (m: string) => void;
}) {
  const { data, loading, failure, refetch } = useApi().get<ApiCodeSchemaResponse>(path);
  const [query, setQuery] = useState("");

  const all = useMemo(() => data?.tables ?? [], [data]);
  const matching = useMemo(() => all.filter((t) => tableMatches(t, query)), [all, query]);

  // Tables and views group together rather than in two passes: the directory
  // they live in already separates them wherever a project separates them
  // (02_tables/ beside 03_views/), and where it does not, a file that declares
  // a table and its view is one unit.
  const tables = matching.filter((t) => t.node.kind !== "view");
  const views = matching.filter((t) => t.node.kind === "view");
  const sections = useMemo(() => groupTables(matching), [matching]);

  // Routines and types answer to the same filter box as the tables — a filter
  // that silently applied to only part of the page would be worse than none.
  const allRoutines = useMemo(() => data?.routines ?? [], [data]);
  const allTypes = useMemo(() => data?.types ?? [], [data]);
  const routines = useMemo(
    () => allRoutines.filter((r) => nodeMatches(r.node.name, query)),
    [allRoutines, query],
  );
  const sqlTypes = useMemo(
    () => allTypes.filter((t) => nodeMatches(t.name, query)),
    [allTypes, query],
  );

  const totalAll = all.length + allRoutines.length + allTypes.length;
  const totalMatching = matching.length + routines.length + sqlTypes.length;
  const summary = query
    ? `${totalMatching} of ${totalAll} match`
    : [
        `${tables.length} ${tables.length === 1 ? "table" : "tables"}`,
        views.length ? `${views.length} ${views.length === 1 ? "view" : "views"}` : "",
        allRoutines.length ? `${allRoutines.length} routines` : "",
        allTypes.length ? `${allTypes.length} types` : "",
      ]
        .filter(Boolean)
        .join(", ");

  const empty = data && !data.degraded && totalAll === 0;

  // The rail is mounted in the Shell aside, outside this Outlet subtree, and
  // this route has no /api/rail payload to drive it — so the index it renders
  // is published here. Cleared on unmount so leaving the route does not leave
  // a stale tree behind in the rail.
  //
  // Only ever published from a loaded response. The fetch hook holds `data`
  // null while a request is in flight and nulls it again on any failed or
  // non-ok refetch, so publishing unconditionally emptied the rail on every
  // member switch and every transient refetch failure — which is what made
  // the index look like it vanished at random.
  useEffect(() => {
    if (!data) return;

    const entries = sections.map((s) => ({
      id: s.id,
      title: s.title,
      count: s.count,
      children: s.groups.map((g) => ({ id: g.id, title: g.title, count: g.tables.length })),
    }));
    if (routines.length) {
      entries.push({ id: "schema-routines", title: "Routines", count: routines.length, children: [] });
    }
    if (sqlTypes.length) {
      entries.push({ id: "schema-types", title: "Types", count: sqlTypes.length, children: [] });
    }
    emitSchemaIndex({ member, sections: entries });
    return () => emitSchemaIndex({ member: "", sections: [] });
  }, [data, sections, routines, sqlTypes, member]);

  return (
    <div className="page-content-inner code-schema" data-route="schema">
      <h2 className="code-schema-title">SQL Schema</h2>
      {/* The view is derived, not authored: without saying so it reads as a
          database connection rather than what the indexer found in the repo's
          own .sql files. */}
      <p className="code-schema-lede">
        Every table and view the code index found in this repository&rsquo;s SQL files, with the
        columns, keys and indexes declared on each — plus which queries read it and which write to
        it. Built from the source in the repo, not from a live database connection.
      </p>

      {/* The toolbar outlives the empty and failure states, because the member
          picker lives in it: hiding it on a member with no SQL stranded the
          reader on that member with no control to switch off it — and in a
          realm, "this one has no SQL" is the common case, not the edge. The
          controls that act on rows are dropped in that state; the picker and
          the rebuild button are exactly what is still useful. */}
      {members.length > 1 || (!empty && !failure) ? (
        <SchemaToolbar
          members={members}
          member={member}
          setMember={setMember}
          query={query}
          setQuery={setQuery}
          onReindexed={refetch}
          summary={summary}
          empty={empty || !!failure}
        />
      ) : null}

      {loading && !data ? <p className="loading">Loading…</p> : null}
      {failure ? <p className="code-schema-empty">Schema unavailable — is this member indexed?</p> : null}
      {data?.degraded ? (
        <p className="code-schema-empty">This member is not indexed — run `atomic code index` in it.</p>
      ) : null}

      {/* Most projects have no SQL at all, and for them this view is
          permanently empty — say what it would have shown and point at the
          surfaces that do cover their code, rather than leaving a blank page
          that reads as a failure. */}
      {empty ? (
        <div className="code-schema-empty-state">
          <p className="code-schema-empty-title">No SQL in this index</p>
          <p>
            This view lists the tables and views the indexer found in <code>.sql</code> files. This
            {members.length > 1 ? " member" : " repository"} has none, so there is nothing to show —
            it is not an error.
          </p>
          <p>
            For non-SQL code, the <Link to="/graph?view=code">code graph</Link> and{" "}
            <Link to="/search">code search</Link> cover the same index: functions, types, imports,
            and what calls what.
          </p>
        </div>
      ) : null}

      {!empty && totalMatching === 0 && totalAll > 0 ? (
        <p className="code-schema-empty">Nothing matches “{query}”.</p>
      ) : null}

      {totalMatching > 0 ? (
        <div className="code-schema-main">
          {sections.map((s) => (
            <DirSection key={s.id} section={s} member={member} />
          ))}

          {routines.length > 0 ? (
            <section className="code-schema-section" id="schema-routines">
              <h3 className="code-schema-section-title">
                Routines
                <span className="code-schema-group-count">{routines.length}</span>
              </h3>
              <Masonry className="code-schema-grid" deps={[routines, member]}>
                {routines.map((r) => (
                  <RoutineCard key={r.node.id} routine={r} member={member} />
                ))}
              </Masonry>
            </section>
          ) : null}

          {/* Domains and composite types: what the column type column is
              actually naming when it says TG_NO. */}
          {sqlTypes.length > 0 ? (
            <section className="code-schema-section" id="schema-types">
              <h3 className="code-schema-section-title">
                Types
                <span className="code-schema-group-count">{sqlTypes.length}</span>
              </h3>
              <div className="code-schema-types">
                {sqlTypes.map((t) => (
                  <span key={t.id} className="code-schema-type">
                    <NodeLink node={t} member={member} />
                    {t.dataType ? <code className="code-schema-column-type">{t.dataType}</code> : null}
                  </span>
                ))}
              </div>
            </section>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}
