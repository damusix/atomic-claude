// GraphLayoutToggle — force layout or stratified tree.
//
// The engine owns both layouts (GraphCore.setLayout); this is the switch. The
// force layout answers "what clusters with what", the tree answers "how far is
// everything from the hubs" — neither subsumes the other, which is why this is
// a toggle rather than a better default.
import { useEffect, useState } from "react";

type Layout = "force" | "tree";

interface GraphCoreLayout {
  setLayout?: (mode: Layout) => void;
}

function engine(): GraphCoreLayout | null {
  return (window as unknown as { GraphCore?: GraphCoreLayout }).GraphCore ?? null;
}

export function GraphLayoutToggle({ resetKey }: { resetKey: string }) {
  const [layout, setLayout] = useState<Layout>("force");

  // Switching view or member remounts the graph, and a fresh mount is always
  // force — the button would otherwise keep claiming "tree" over a force
  // layout it no longer controls.
  useEffect(() => {
    setLayout("force");
  }, [resetKey]);

  const apply = (next: Layout) => {
    setLayout(next);
    engine()?.setLayout?.(next);
  };

  return (
    <span className="search-toggle graph-layout-switch" role="group" aria-label="Graph layout">
      {(["force", "tree"] as Layout[]).map((mode) => (
        <button
          key={mode}
          type="button"
          className={`toggle-btn${layout === mode ? " toggle-active" : ""}`}
          aria-pressed={layout === mode}
          onClick={() => apply(mode)}
        >
          {mode === "force" ? "Force" : "Tree"}
        </button>
      ))}
    </span>
  );
}
