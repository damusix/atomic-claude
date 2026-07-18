// SchemaView — SQL schema view (tables/views/columns/FK sources/writers),
// mounted at the /code/schema route (pages/Schema). Reuses the
// /code/graph/members member picker convention pages/Graph/Graph.tsx already
// established (carried, non-/api/* endpoint — utils/graphEngineAdapter's
// fetchGraphMembers/resolveMember), since /api/code/schema itself carries no
// member list — only the selected member's tables. Node/column/FK/writer
// names open the code modal via the store.ts openNode seam, mirroring
// codeexplorer.go's renderTableSchema drill-down links.
import { useEffect, useState } from "react";
import { openNode } from "../code-modal/store";
import type { ApiCodeNode } from "../code-modal/types";
import { useApi } from "../../utils/api";
import { fetchGraphMembers, resolveMember, type GraphMember } from "../../utils/graphEngineAdapter";
import type { ApiCodeSchemaResponse, ApiTableSchema } from "./types";
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

function TableCard({ table, member }: { table: ApiTableSchema; member: string }) {
  return (
    <div className="code-schema-table">
      <h4 className="code-schema-table-name">
        <NodeLink node={table.node} member={member} />
        {table.node.filePath ? (
          <span className="code-schema-table-loc">
            {" "}
            {table.node.filePath}
            {table.node.startLine > 0 ? `:${table.node.startLine}` : ""}
          </span>
        ) : null}
      </h4>

      {table.columns.length > 0 ? (
        <ul className="code-schema-columns">
          {table.columns.map((col) => (
            <li key={col.id} className="code-schema-column">
              {col.name}
              {col.signature ? <code> {col.signature}</code> : null}
            </li>
          ))}
        </ul>
      ) : null}

      {table.fkSources.length > 0 ? (
        <div className="code-schema-fk-sources">
          <span className="code-schema-fk-label">Referenced by:</span>{" "}
          {table.fkSources.map((src, i) => (
            <span key={src.id}>
              {i > 0 ? ", " : ""}
              <NodeLink node={src} member={member} />
            </span>
          ))}
        </div>
      ) : null}

      {table.writers.length > 0 ? (
        <div className="code-schema-writers">
          <span className="code-schema-writers-label">Writers:</span>{" "}
          {table.writers.map((w, i) => (
            <span key={w.id}>
              {i > 0 ? ", " : ""}
              <NodeLink node={w} member={member} />
            </span>
          ))}
        </div>
      ) : null}
    </div>
  );
}

function SchemaSection({ title, tables, member }: { title: string; tables: ApiTableSchema[]; member: string }) {
  if (tables.length === 0) return null;
  return (
    <section className="code-schema-section">
      <h3 className="code-schema-section-title">{title}</h3>
      {tables.map((t) => (
        <TableCard key={t.node.id} table={t} member={member} />
      ))}
    </section>
  );
}

export function SchemaView() {
  const [members, setMembers] = useState<GraphMember[]>([]);
  const [member, setMember] = useState("");

  useEffect(() => {
    let cancelled = false;
    void fetchGraphMembers().then((fetched) => {
      if (cancelled) return;
      setMembers(fetched);
      setMember((prev) => resolveMember(fetched, prev));
    });
    return () => {
      cancelled = true;
    };
  }, []);

  const path = member ? `/code/schema?member=${encodeURIComponent(member)}` : "/code/schema";
  const { data, loading, failure } = useApi().get<ApiCodeSchemaResponse>(path);

  const tables = data?.tables.filter((t) => t.node.kind !== "view") ?? [];
  const views = data?.tables.filter((t) => t.node.kind === "view") ?? [];

  return (
    <div className="code-schema" data-route="schema">
      <h2 className="code-schema-title">SQL Schema</h2>

      {members.length > 1 ? (
        <select
          className="code-schema-member-select"
          aria-label="Code member"
          value={member}
          onChange={(e) => setMember(e.target.value)}
        >
          {members.map((m) => (
            <option key={m.prefix} value={m.prefix}>
              {memberLabel(m)}
            </option>
          ))}
        </select>
      ) : null}

      {loading && !data ? <p className="loading">Loading…</p> : null}
      {failure ? <p className="code-schema-empty">Schema unavailable — is this member indexed?</p> : null}
      {data && tables.length === 0 && views.length === 0 ? (
        <p className="code-schema-empty">No SQL schema found in this index.</p>
      ) : null}

      <SchemaSection title="Tables" tables={tables} member={member} />
      <SchemaSection title="Views" tables={views} member={member} />
    </div>
  );
}
