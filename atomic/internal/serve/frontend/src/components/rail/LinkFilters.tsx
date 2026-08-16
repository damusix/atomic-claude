// LinkFilters — search box and type chips over the Links tab.
//
// A page can carry several hundred outbound links, where paging through
// "showing 20 of 384" is the only way to reach anything. Filtering answers
// the questions actually asked of that list: "does this page link to X" and
// "what Go files does it touch".
import { useState } from "react";
import { faXmark } from "@fortawesome/free-solid-svg-icons";
import { FaGlyph } from "../ui";

export interface KindCount {
  kind: string;
  count: number;
}

/** Types shown before the row collapses. A page touching twenty extensions
    turns the chip row into a four-line wall above the list it filters — the
    long tail is one or two links each and is reachable by search instead. */
const VISIBLE_KINDS = 8;

export function LinkFilters({
  query,
  onQuery,
  kind,
  onKind,
  kinds,
  total,
  shown,
}: {
  query: string;
  onQuery: (next: string) => void;
  kind: string;
  onKind: (next: string) => void;
  kinds: KindCount[];
  total: number;
  shown: number;
}) {
  const [allKinds, setAllKinds] = useState(false);
  const filtering = query !== "" || kind !== "";
  const hiddenKinds = Math.max(0, kinds.length - VISIBLE_KINDS);
  // A selected type stays visible even when it sits in the collapsed tail —
  // otherwise the active filter disappears from its own control.
  const visibleKinds =
    allKinds || hiddenKinds === 0
      ? kinds
      : kinds.slice(0, VISIBLE_KINDS).concat(
          kinds.slice(VISIBLE_KINDS).filter((k) => k.kind === kind),
        );

  return (
    <div className="rail-filters">
      <div className="rail-search">
        <input
          type="search"
          className="rail-search-input"
          placeholder="Filter links…"
          aria-label="Filter links"
          value={query}
          onChange={(e) => onQuery(e.target.value)}
        />
        {query ? (
          <button
            type="button"
            className="rail-search-clear"
            aria-label="Clear filter"
            onClick={() => onQuery("")}
          >
            <FaGlyph icon={faXmark} size={10} />
          </button>
        ) : null}
      </div>

      {/* Only the types actually present, so the row never offers a filter
          that would empty the list. */}
      {kinds.length > 1 ? (
        <div className="rail-kinds" role="group" aria-label="Filter by type">
          <button
            type="button"
            className="rail-kind"
            data-on={kind === "" || undefined}
            onClick={() => onKind("")}
          >
            all
          </button>
          {visibleKinds.map((k) => (
            <button
              key={k.kind}
              type="button"
              className="rail-kind"
              data-on={kind === k.kind || undefined}
              onClick={() => onKind(kind === k.kind ? "" : k.kind)}
            >
              {k.kind}
              <span className="rail-kind-count">{k.count}</span>
            </button>
          ))}
          {hiddenKinds > 0 ? (
            <button
              type="button"
              className="rail-kind rail-kind-more"
              onClick={() => setAllKinds((v) => !v)}
            >
              {allKinds ? "less" : `+${hiddenKinds}`}
            </button>
          ) : null}
        </div>
      ) : null}

      {filtering ? (
        <p className="rail-filter-result">
          {shown} of {total} match
        </p>
      ) : null}
    </div>
  );
}
