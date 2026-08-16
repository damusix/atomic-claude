// EdgeList — one outbound-link row per resolved target. Each of the five link
// kinds routes differently (broken links are inert text, external opens a new
// tab, code opens the file modal, directories and pages navigate in-SPA), which
// is why this is a switch on the edge rather than one <a> with a computed href.
import { useState } from "react";
import { faFolder, faLinkSlash, faUpRightFromSquare } from "@fortawesome/free-solid-svg-icons";
import { openFile } from "../code-modal/store";
import { FaGlyph, FileIcon, Tooltip } from "../ui";
import type { EdgeView } from "./edges";

/** Rows shown before the list collapses behind its own count. */
export const COLLAPSED_LIMIT = 20;

/** What the tooltip shows: where the link actually lands, and how it was
    written when the two differ (a "../../.." that resolves to the root). */
function detail(view: EdgeView) {
  const { edge } = view;
  if (edge.broken) return `unresolved: ${edge.target}`;
  if (edge.external) return edge.resolvedPath || edge.target;
  const resolved = edge.resolvedPath || edge.target;
  return resolved === edge.target ? resolved : `${resolved}  ←  ${edge.target}`;
}

// Every row carries a glyph, not just directories: a column of icons is what
// makes the list scannable by kind instead of by reading each filename.
function RowIcon({ view }: { view: EdgeView }) {
  const { edge } = view;
  if (edge.broken) return <FaGlyph icon={faLinkSlash} size={10} className="rail-edge-glyph" />;
  if (edge.external) {
    return <FaGlyph icon={faUpRightFromSquare} size={9} className="rail-edge-glyph" />;
  }
  if (edge.dir) return <FaGlyph icon={faFolder} size={10} className="rail-edge-glyph" />;
  return (
    <FileIcon relpath={edge.resolvedPath || edge.target} className="rail-edge-glyph" />
  );
}

function Row({ view }: { view: EdgeView }) {
  const { edge, name, context, anchor } = view;
  const body = (
    <>
      <RowIcon view={view} />
      <span className="rail-edge-name">{name}</span>
      {context ? <span className="rail-edge-context">{context}</span> : null}
    </>
  );

  if (edge.broken) {
    return (
      <Tooltip label={detail(view)} placement="left">
        <span className="rail-edge wikilink-broken">{body}</span>
      </Tooltip>
    );
  }

  if (edge.external) {
    return (
      <Tooltip label={detail(view)} placement="left">
        <a
          className="rail-edge"
          href={edge.resolvedPath || edge.target}
          target="_blank"
          rel="noopener noreferrer"
        >
          {body}
        </a>
      </Tooltip>
    );
  }

  if (edge.codeFile) {
    return (
      <Tooltip label={detail(view)} placement="left">
        <a
          className="rail-edge"
          href={`/file/${edge.resolvedPath}`}
          onClick={(ev) => {
            ev.preventDefault();
            openFile(edge.resolvedPath);
          }}
        >
          {body}
        </a>
      </Tooltip>
    );
  }

  return (
    <Tooltip
      label={edge.ambiguous ? `${detail(view)} — ambiguous, several files match` : detail(view)}
      placement="left"
    >
      <a
        className={`rail-edge wikilink${edge.ambiguous ? " wikilink-ambiguous" : ""}`}
        href={`/page/${edge.resolvedPath}${anchor}`}
      >
        {body}
      </a>
    </Tooltip>
  );
}

/** The count line doubles as the expand control — a truncated list that gives
    no way to see the rest is just missing data. */
export function ExpandToggle({
  shown,
  total,
  expanded,
  onToggle,
}: {
  shown: number;
  total: number;
  expanded: boolean;
  onToggle: () => void;
}) {
  return (
    <button type="button" className="rail-edge-more" onClick={onToggle}>
      {expanded ? `showing all ${total} −` : `showing ${shown} of ${total} +`}
    </button>
  );
}

export function EdgeList({ views, limit = COLLAPSED_LIMIT }: { views: EdgeView[]; limit?: number }) {
  const [expanded, setExpanded] = useState(false);
  const capped = views.length > limit;
  const shown = capped && !expanded ? views.slice(0, limit) : views;

  return (
    <>
      <ul className="rail-edge-list">
        {shown.map((view) => (
          <li key={view.edge.resolvedPath || view.edge.target}>
            <Row view={view} />
          </li>
        ))}
      </ul>
      {capped ? (
        <ExpandToggle
          shown={limit}
          total={views.length}
          expanded={expanded}
          onToggle={() => setExpanded((v) => !v)}
        />
      ) : null}
    </>
  );
}
