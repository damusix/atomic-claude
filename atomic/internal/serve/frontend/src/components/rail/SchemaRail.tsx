// SchemaRail — the schema route's navigation, in the rail where every other
// route's navigation lives. It reads components/schema's published index off
// the bus rather than fetching anything: the schema route has no page, so
// /api/rail has nothing to say about it.
//
// Directories and the files inside them, not table names. The table names are
// already on screen in the grid — repeating all 130 of them in a second column
// is a longer list, not a way through one.
import { useEffect, useState } from "react";
import { events, getSchemaIndex, type SchemaIndex, type SchemaIndexEntry } from "../../utils/events";

function jump(id: string) {
  // scroll-margin-top on the target keeps the heading clear of the top bar.
  document.getElementById(id)?.scrollIntoView({ block: "start", behavior: "smooth" });
}

function Entry({ entry, depth }: { entry: SchemaIndexEntry; depth: number }) {
  return (
    <li>
      <a
        className="rail-schema-link"
        data-depth={depth}
        href={`#${entry.id}`}
        onClick={(e) => {
          // A hash push would send the router looking for a route; scroll the
          // section into view directly instead.
          e.preventDefault();
          jump(entry.id);
        }}
      >
        <span className="rail-schema-label">{entry.title}</span>
        <span className="rail-schema-count">{entry.count}</span>
      </a>
      {entry.children?.length ? (
        <ul className="rail-schema-children">
          {entry.children.map((child) => (
            <Entry key={child.id} entry={child} depth={depth + 1} />
          ))}
        </ul>
      ) : null}
    </li>
  );
}

export function SchemaRail() {
  const [index, setIndex] = useState<SchemaIndex>(() => getSchemaIndex());

  useEffect(() => events.on("schema.index", (next) => setIndex(next)), []);

  const total = index.sections.reduce((n, s) => n + s.count, 0);

  return (
    <div className="rail-schema">
      <div className="rail-slot-label">
        Schema
        {total > 0 ? <span className="rail-schema-total">{total}</span> : null}
      </div>
      {index.sections.length ? (
        <ul className="rail-schema-list">
          {index.sections.map((s) => (
            <Entry key={s.id} entry={s} depth={0} />
          ))}
        </ul>
      ) : (
        <span className="rail-empty">nothing indexed</span>
      )}
    </div>
  );
}
