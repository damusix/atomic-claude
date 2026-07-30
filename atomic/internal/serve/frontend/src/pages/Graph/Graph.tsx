// Graph route ("/graph?view=&member="): Docs|Code switcher + member picker,
// mounting the carried cosmos.gl engine via utils/graphEngineAdapter. Ported
// from system-graph.js's renderGraphPane()/mountCodeView()/enterGraphMode()
// shell-orchestration (htmx-era DOM rebuilds, not part of the carried
// engine) — see docs/spec/serve-react-frontend.md's "Flow: graph-mode mount".
import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import {
  fetchGraphMembers,
  type GraphMember,
  type GraphView,
  mountGraph,
  resolveMember,
  teardownGraph,
} from "../../utils/graphEngineAdapter";

function memberLabel(m: GraphMember): string {
  return (m.prefix || "(local)") + (m.indexed ? "" : " — not indexed");
}

export function Graph() {
  const [searchParams, setSearchParams] = useSearchParams();
  const view: GraphView = searchParams.get("view") === "code" ? "code" : "docs";
  const memberParam = searchParams.get("member") ?? "";
  const [members, setMembers] = useState<GraphMember[]>([]);
  const containerRef = useRef<HTMLDivElement>(null);

  // The container carries a key derived from view+member (below, in the
  // JSX) so a switch forces React to create a FRESH DOM node — mirroring
  // renderGraphPane's own full mainPane.innerHTML rebuild. graph-core.js's
  // mount() guards against a double-mount on the SAME container
  // (container.dataset.systemMounted) but never clears that flag on
  // teardown, since the carried htmx-era caller always handed it a fresh
  // container too; reusing one DOM node across a switch would make every
  // mount after the first a silent no-op.
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    let cancelled = false;

    async function run(container: HTMLElement) {
      if (view === "code") {
        const fetched = await fetchGraphMembers();
        if (cancelled) return;
        setMembers(fetched);

        const resolved = resolveMember(fetched, memberParam);
        if (resolved !== memberParam) {
          setSearchParams(
            (prev) => {
              const next = new URLSearchParams(prev);
              next.set("view", "code");
              if (resolved) next.set("member", resolved);
              else next.delete("member");
              return next;
            },
            { replace: true },
          );
          return; // effect re-runs once the corrected params land
        }
        await mountGraph(container, "code", resolved || undefined);
      } else {
        setMembers([]);
        await mountGraph(container, "docs");
      }
    }
    void run(container);

    return () => {
      cancelled = true;
      teardownGraph(view);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [view, memberParam]);

  function switchView(next: GraphView) {
    if (next === view) return;
    setSearchParams((prev) => {
      const params = new URLSearchParams(prev);
      if (next === "code") params.set("view", "code");
      else params.delete("view");
      params.delete("member");
      return params;
    });
  }

  function switchMember(prefix: string) {
    setSearchParams((prev) => {
      const params = new URLSearchParams(prev);
      params.set("view", "code");
      params.set("member", prefix);
      return params;
    });
  }

  return (
    <div className="graph-route" data-route="graph">
      <div className="graph-pane-controls" id="graph-pane-controls">
        <span className="search-toggle graph-view-switch" role="group" aria-label="Graph view">
          <button
            type="button"
            className={`toggle-btn${view === "docs" ? " toggle-active" : ""}`}
            data-graph-view="docs"
            aria-pressed={view === "docs"}
            onClick={() => switchView("docs")}
          >
            Docs
          </button>
          <button
            type="button"
            className={`toggle-btn${view === "code" ? " toggle-active" : ""}`}
            data-graph-view="code"
            aria-pressed={view === "code"}
            onClick={() => switchView("code")}
          >
            Code
          </button>
        </span>
        <span id="graph-member-picker-slot">
          {view === "code" && members.length > 1 && (
            <select
              id="graph-member-select"
              className="graph-member-select"
              aria-label="Code member"
              value={memberParam}
              onChange={(e) => switchMember(e.target.value)}
            >
              {members.map((m) => (
                <option key={m.prefix} value={m.prefix}>
                  {memberLabel(m)}
                </option>
              ))}
            </select>
          )}
        </span>
      </div>
      <div
        key={`${view}:${memberParam}`}
        ref={containerRef}
        id={view === "code" ? "code-cy" : "system-cy"}
        data-code-graph={view === "code" ? "" : undefined}
        data-system-graph={view === "docs" ? "" : undefined}
        className="graph-mount"
      />
      <p className="loading system-graph-loading">Laying out graph…</p>
    </div>
  );
}
