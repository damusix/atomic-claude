// GraphSearch — find nodes by name in the mounted graph.
//
// The engine owns the emphasis (GraphCore.search highlights matches and dims
// the rest); this is only the input and the result readout. Locating a node
// by eye in a graph of thousands is not a thing anyone can do, which is what
// makes this the difference between the view being explorable and being
// decorative.
import { useEffect, useRef, useState } from "react";
import { faXmark } from "@fortawesome/free-solid-svg-icons";
import { FaGlyph } from "../../components/ui";

interface GraphCoreSearch {
  search?: (query: string) => number;
}

function engine(): GraphCoreSearch | null {
  const core = (window as unknown as { GraphCore?: GraphCoreSearch }).GraphCore;
  return core ?? null;
}

export function GraphSearch({ resetKey }: { resetKey: string }) {
  const [query, setQuery] = useState("");
  const [matches, setMatches] = useState<number | null>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // Switching view or member remounts the graph, which drops the previous
  // mount's highlight state — the box would otherwise still show a query
  // that no longer emphasises anything.
  useEffect(() => {
    setQuery("");
    setMatches(null);
  }, [resetKey]);

  useEffect(() => {
    // Debounced: each query is a linear scan over every node label, and
    // running it per keystroke on a 20k-node graph stutters the input.
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      const core = engine();
      if (!core?.search) return;
      setMatches(query.trim() ? core.search(query) : null);
    }, 140);

    return () => {
      if (timer.current) clearTimeout(timer.current);
    };
  }, [query]);

  // Leaving the page with a highlight applied would strand it on the next
  // mount of the same engine.
  useEffect(() => {
    return () => {
      engine()?.search?.("");
    };
  }, []);

  return (
    <span className="graph-search">
      <input
        type="search"
        className="graph-search-input"
        placeholder="Find a node…"
        aria-label="Find a node in the graph"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      {query ? (
        <button
          type="button"
          className="graph-search-clear"
          aria-label="Clear graph search"
          onClick={() => setQuery("")}
        >
          <FaGlyph icon={faXmark} size={10} />
        </button>
      ) : null}
      {matches !== null ? (
        <span className="graph-search-count" data-empty={matches === 0 || undefined}>
          {matches === 0 ? "no matches" : `${matches} match${matches === 1 ? "" : "es"}`}
        </span>
      ) : null}
    </span>
  );
}
