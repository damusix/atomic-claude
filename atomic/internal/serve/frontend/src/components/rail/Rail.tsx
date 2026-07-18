// Rail — Properties / this-page mini-graph / OUT / IN panels, mounted once
// in Shell's #right-rail aside (outside pages/Page's Outlet subtree). Listens
// for "page.resolved" (utils/events) instead of reading the route directly:
// pages/Page emits the server-RESOLVED relpath after /api/page succeeds
// (directory URLs resolve to their index file), which is the relpath
// /api/rail must be queried with — the raw URL param is not always it.
import { useEffect, useState } from "react";
import { attempt } from "@logosdx/utils";
import { openFile } from "../code-modal/store";
import { shouldRefetchPage } from "../../hooks/useLiveReload";
import { api } from "../../utils/api";
import { events } from "../../utils/events";
import { MiniGraph } from "./MiniGraph";
import type { RailResponse } from "./types";
import "./style.css";

export function Rail() {
  const [relpath, setRelpath] = useState<string | null>(null);
  const [rail, setRail] = useState<RailResponse | null>(null);

  useEffect(() => {
    return events.on("page.resolved", ({ relpath: next }) => setRelpath(next));
  }, []);

  useEffect(() => {
    let cancelled = false;
    if (!relpath) {
      setRail(null);
      return;
    }

    function fetchRail() {
      void attempt(() => api.get<RailResponse>(`/rail/${relpath}`)).then(([res, err]) => {
        // api.get() resolves (never rejects) on a server-said-no response —
        // res.ok distinguishes a real payload (200) from the {error}
        // envelope a 404 (no graph membership) carries.
        if (!cancelled) setRail(!err && res?.ok ? res.data : null);
      });
    }

    fetchRail();
    // Live-reload reconcile (spec Flow): rail follows the same open-relpath
    // conditional as pages/Page's own subscription — a separate subscription
    // rather than a shared trigger since Rail is mounted outside Page's
    // subtree (Shell's aside) and owns its own fetch/state independently.
    const off = events.on("realm.changed", ({ changed }) => {
      if (shouldRefetchPage(relpath, changed)) fetchRail();
    });

    return () => {
      cancelled = true;
      off();
    };
  }, [relpath]);

  if (!relpath || !rail) {
    return <aside id="right-rail" aria-label="Rail" />;
  }

  return (
    <aside id="right-rail" aria-label="Rail">
      <section id="rail-props" className="rail-slot">
        <div className="rail-slot-content" id="rail-props-content">
          {rail.properties?.length ? (
            <ul className="rail-props-list">
              {rail.properties.map((p) => (
                <li key={p.key} className={p.isJSON ? "rail-prop-li-json" : undefined}>
                  <span className="rail-prop-key">{p.key}</span>
                  <span className="rail-prop-val">
                    {p.isJSON ? (
                      <pre className="rail-prop-json">
                        <code>{p.value}</code>
                      </pre>
                    ) : p.isURL ? (
                      <a href={p.value} target="_blank" rel="noopener">
                        {p.value}
                      </a>
                    ) : (
                      p.value
                    )}
                  </span>
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      </section>

      <section id="rail-graph" className="rail-slot">
        <div className="rail-slot-content" id="rail-graph-content">
          <MiniGraph graphDataURL={rail.graphDataURL} focusNode={relpath} />
        </div>
      </section>

      <section id="rail-out" className="rail-slot">
        <div className="rail-slot-content" id="rail-out-content">
          {rail.out?.length ? (
            <ul className="rail-edge-list">
              {rail.out.map((e, i) => (
                <li key={`${e.target}:${i}`}>
                  {e.broken ? (
                    <span className="wikilink-broken" title={`unresolved: ${e.target}`}>
                      {e.target}
                    </span>
                  ) : e.external ? (
                    <a href={e.resolvedPath || e.target} target="_blank" rel="noopener noreferrer">
                      {e.target}
                    </a>
                  ) : e.codeFile ? (
                    <a
                      href={`/file/${e.resolvedPath}`}
                      onClick={(ev) => {
                        ev.preventDefault();
                        openFile(e.resolvedPath);
                      }}
                    >
                      {e.target}
                    </a>
                  ) : (
                    <a
                      href={`/page/${e.resolvedPath}`}
                      className={e.ambiguous ? "wikilink wikilink-ambiguous" : "wikilink"}
                      title={e.ambiguous ? "ambiguous: multiple files match" : undefined}
                    >
                      {e.target}
                    </a>
                  )}
                </li>
              ))}
            </ul>
          ) : (
            <span className="rail-empty">no outbound links</span>
          )}
        </div>
      </section>

      <section id="rail-in" className="rail-slot">
        <div className="rail-slot-content" id="rail-in-content">
          {rail.in?.length ? (
            <ul className="rail-edge-list">
              {rail.in.map((b) => (
                <li key={b.path}>
                  <a href={`/page/${b.path}`} className="wikilink">
                    {b.path}
                  </a>
                </li>
              ))}
            </ul>
          ) : (
            <span className="rail-empty">{rail.orphan ? "orphan — no backlinks" : "no backlinks"}</span>
          )}
        </div>
      </section>
    </aside>
  );
}
