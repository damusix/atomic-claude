// GraphReindex — rebuild the code index for the member being graphed.
//
// Same control the schema page carries, over the same useReindex hook: a
// graph drawn from a stale index is wrong in a way the reader cannot see, and
// the fix was previously only reachable by navigating to another page.
import { faArrowsRotate } from "@fortawesome/free-solid-svg-icons";
import { useReindex } from "../../hooks/useReindex";
import { FaGlyph } from "../../components/ui";

const LABEL: Record<string, string> = {
  idle: "Reindex",
  running: "Reindexing…",
  done: "Reindexed",
  failed: "Retry reindex",
};

export function GraphReindex({ member, onReindexed }: { member: string; onReindexed: () => void }) {
  const reindex = useReindex(member, onReindexed);

  return (
    <>
      <button
        type="button"
        className="graph-reindex"
        data-state={reindex.state}
        disabled={reindex.state === "running"}
        onClick={() => void reindex.start()}
        title="Rebuild this member's code index from source"
      >
        <FaGlyph icon={faArrowsRotate} size={11} />
        {LABEL[reindex.state] ?? "Reindex"}
      </button>
      {reindex.error ? <span className="graph-reindex-error">{reindex.error}</span> : null}
    </>
  );
}
