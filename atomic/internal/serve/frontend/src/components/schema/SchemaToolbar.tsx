// SchemaToolbar — member picker, filter, and the rebuild control.
import { faArrowsRotate, faXmark } from "@fortawesome/free-solid-svg-icons";
import { FaGlyph } from "../ui";
import { pickerLabel, type GraphMember } from "../../utils/graphEngineAdapter";
import { useReindex } from "../../hooks/useReindex";

const REINDEX_LABEL: Record<string, string> = {
  idle: "Reindex",
  running: "Reindexing…",
  done: "Reindexed",
  failed: "Retry reindex",
};

export function SchemaToolbar({
  members,
  member,
  setMember,
  realmName,
  query,
  setQuery,
  onReindexed,
  summary,
  empty = false,
}: {
  members: GraphMember[];
  member: string;
  setMember: (m: string) => void;
  realmName: string;
  query: string;
  setQuery: (q: string) => void;
  onReindexed: () => void;
  summary: string;
  /** No schema to show for this member. The toolbar still renders — the
      member picker is the only way off a member with no SQL — but the
      controls that act on rows have nothing to act on. */
  empty?: boolean;
}) {
  const reindex = useReindex(member, onReindexed);

  return (
    <div className="code-schema-toolbar">
      {members.length > 1 ? (
        <label className="code-schema-field">
          <span className="code-schema-field-label">Member</span>
          <span className="code-schema-select-wrap">
            <select
              className="code-schema-member-select"
              aria-label="Code member"
              value={member}
              onChange={(e) => setMember(e.target.value)}
            >
              {members.map((m) => (
                <option key={m.prefix} value={m.prefix}>
                  {pickerLabel(m, realmName)}
                </option>
              ))}
            </select>
          </span>
        </label>
      ) : null}

      {empty ? null : (
      <label className="code-schema-field code-schema-field-grow">
        <span className="code-schema-field-label">Filter</span>
        <span className="code-schema-search">
          <input
            type="search"
            className="code-schema-search-input"
            placeholder="Table or column name…"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
          />
          {query ? (
            <button
              type="button"
              className="code-schema-search-clear"
              aria-label="Clear filter"
              onClick={() => setQuery("")}
            >
              <FaGlyph icon={faXmark} size={10} />
            </button>
          ) : null}
        </span>
      </label>
      )}

      <div className="code-schema-toolbar-end">
        {empty ? null : <span className="code-schema-summary">{summary}</span>}
        <button
          type="button"
          className="code-schema-reindex"
          data-state={reindex.state}
          disabled={reindex.state === "running"}
          onClick={() => void reindex.start()}
          title="Rebuild this member's code index from source"
        >
          <FaGlyph icon={faArrowsRotate} size={11} />
          {REINDEX_LABEL[reindex.state] ?? "Reindex"}
        </button>
      </div>

      {reindex.error ? <p className="code-schema-reindex-error">{reindex.error}</p> : null}
    </div>
  );
}
