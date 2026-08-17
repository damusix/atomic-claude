// Rail — the page inspector, mounted once in Shell's #right-rail aside
// (outside pages/Page's Outlet subtree). Listens for "page.resolved"
// (utils/events) instead of reading the route directly: pages/Page emits the
// server-RESOLVED relpath after /api/page succeeds (directory URLs resolve to
// their index file), which is the relpath /api/rail must be queried with —
// the raw URL param is not always it.
//
// Three tabs rather than four stacked slots: a page with 49 outbound links
// pushed everything below it off-screen, so relationships live behind a tab
// that carries its own count and the summary stays visible.
import { useEffect, useMemo, useState } from "react";
import { useLocation } from "react-router";
import { Tabs } from "@ark-ui/react";
import { attempt } from "@logosdx/utils";
import { shouldRefetchPage } from "../../hooks/useLiveReload";
import { api } from "../../utils/api";
import { events } from "../../utils/events";
import { BusRail } from "./BusRail";
import { Contents } from "./Contents";
import { JsonValue } from "./JsonValue";
import { COLLAPSED_LIMIT, EdgeList, ExpandToggle } from "./EdgeList";
import { backlinkView, countEdges, dedupeEdges, edgeKind, edgeMatches, pathKind } from "./edges";
import { LinkFilters, type KindCount } from "./LinkFilters";
import { MiniGraph } from "./MiniGraph";
import { SchemaRail } from "./SchemaRail";
import { FileIcon, Tooltip } from "../ui";
import type { RailBacklink, RailResponse } from "./types";
import "./style.css";

function Label({ children }: { children: React.ReactNode }) {
  return <div className="rail-slot-label">{children}</div>;
}

function Stat({ value, label }: { value: number; label: string }) {
  return (
    <div className="rail-stat">
      <b>{value}</b>
      <span>{label}</span>
    </div>
  );
}

function Backlinks({
  links,
  orphan,
  filtered = false,
}: {
  links: RailBacklink[];
  orphan: boolean;
  /** Empty because a filter excluded them, rather than because there are
      none — "orphan" would be a lie about the page. */
  filtered?: boolean;
}) {
  const [expanded, setExpanded] = useState(false);

  if (!links.length) {
    if (filtered) return <span className="rail-empty">no matching backlinks</span>;
    return <span className="rail-empty">{orphan ? "orphan — no backlinks" : "no backlinks"}</span>;
  }

  const capped = links.length > COLLAPSED_LIMIT;
  const shown = capped && !expanded ? links.slice(0, COLLAPSED_LIMIT) : links;

  return (
    <>
      <ul className="rail-edge-list">
        {shown.map((b) => {
          const { name, context } = backlinkView(b.path);
          return (
            <li key={b.path}>
              <Tooltip label={b.path} placement="left">
                <a href={`/page/${b.path}`} className="rail-edge wikilink">
                  <FileIcon relpath={b.path} className="rail-edge-glyph" />
                  <span className="rail-edge-name">{name}</span>
                  {context ? <span className="rail-edge-context">{context}</span> : null}
                </a>
              </Tooltip>
            </li>
          );
        })}
      </ul>
      {capped ? (
        <ExpandToggle
          shown={COLLAPSED_LIMIT}
          total={links.length}
          expanded={expanded}
          onToggle={() => setExpanded((v) => !v)}
        />
      ) : null}
    </>
  );
}

export function Rail() {
  const location = useLocation();
  const [tab, setTab] = useState("overview");
  const [relpath, setRelpath] = useState<string | null>(null);
  const [rail, setRail] = useState<RailResponse | null>(null);
  const [query, setQuery] = useState("");
  const [kind, setKind] = useState("");

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

  // A filter is about the page it was typed on; carrying it to the next one
  // would silently hide most of that page's links.
  useEffect(() => {
    setQuery("");
    setKind("");
  }, [relpath]);

  const out = useMemo(() => dedupeEdges(rail?.out ?? []), [rail]);
  const counts = useMemo(() => countEdges(out), [out]);
  const backlinks = useMemo(() => rail?.in ?? [], [rail]);

  // Both lists answer to one set of controls: a filter that silently applied
  // to only half of what is on screen would be worse than none.
  const filteredOut = useMemo(
    () => out.filter((v) => edgeMatches(v, query) && (!kind || edgeKind(v.edge) === kind)),
    [out, query, kind],
  );
  const filteredBacklinks = useMemo(
    () =>
      backlinks.filter(
        (b) =>
          (!query || b.path.toLowerCase().includes(query.toLowerCase())) &&
          (!kind || pathKind(b.path) === kind),
      ),
    [backlinks, query, kind],
  );

  // Counts come from the unfiltered sets so a chip never reads zero, ordered
  // by weight — the types a page leans on come first.
  const kinds = useMemo<KindCount[]>(() => {
    const tally = new Map<string, number>();
    for (const view of out) {
      const k = edgeKind(view.edge);
      tally.set(k, (tally.get(k) ?? 0) + 1);
    }
    for (const b of backlinks) {
      const k = pathKind(b.path);
      tally.set(k, (tally.get(k) ?? 0) + 1);
    }
    return [...tally.entries()]
      .map(([k, count]) => ({ kind: k, count }))
      .sort((a, b) => b.count - a.count || a.kind.localeCompare(b.kind));
  }, [out, backlinks]);

  // Routes that are not a page swap the page-centric rail for their own
  // navigation — there is no open page to describe on either, and the rail is
  // where this app puts navigation.
  if (location.pathname === "/bus") {
    return (
      <aside id="right-rail" aria-label="Rail">
        <BusRail />
      </aside>
    );
  }

  if (location.pathname === "/code/schema") {
    return (
      <aside id="right-rail" aria-label="Rail">
        <SchemaRail />
      </aside>
    );
  }

  if (!relpath || !rail) {
    return <aside id="right-rail" aria-label="Rail" />;
  }

  return (
    <aside id="right-rail" aria-label="Rail">
      <Tabs.Root
        value={tab}
        onValueChange={(details) => setTab(details.value)}
        className="rail-tabs"
      >
        <Tabs.List className="rail-tab-list">
          <Tabs.Trigger value="overview" className="rail-tab">
            Overview
          </Tabs.Trigger>
          <Tabs.Trigger value="links" className="rail-tab">
            Links <span className="rail-tab-count">{counts.total}</span>
          </Tabs.Trigger>
          <Tabs.Trigger value="graph" className="rail-tab">
            Graph
          </Tabs.Trigger>
          <Tabs.Indicator className="rail-tab-indicator" />
        </Tabs.List>

        <Tabs.Content value="overview" className="rail-panel">
          <section id="rail-props">
            <div className="rail-slot-content" id="rail-props-content">
              {rail.properties?.length ? (
                <ul className="rail-props-list">
                  {rail.properties.map((p) => (
                    <li key={p.key} className={p.isJSON ? "rail-prop-li-json" : undefined}>
                      <span className="rail-prop-key">{p.key}</span>
                      <span className="rail-prop-val">
                        {p.isJSON ? (
                          <JsonValue propKey={p.key} value={p.value} />
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

          <Label>Relationships</Label>
          <div className="rail-stats">
            <Stat value={counts.total} label="outbound" />
            <Stat value={backlinks.length} label="backlinks" />
            <Stat value={counts.code} label="code files" />
            <Stat value={counts.broken} label="unresolved" />
          </div>

          <Label>Contents</Label>
          <Contents />

          <Label>Backlinks</Label>
          <Backlinks links={backlinks} orphan={rail.orphan} />
        </Tabs.Content>

        <Tabs.Content value="links" className="rail-panel">
          <LinkFilters
            query={query}
            onQuery={setQuery}
            kind={kind}
            onKind={setKind}
            kinds={kinds}
            total={out.length + backlinks.length}
            shown={filteredOut.length + filteredBacklinks.length}
          />

          <section id="rail-out">
            <Label>Outbound</Label>
            <div id="rail-out-content">
              {filteredOut.length ? (
                <EdgeList views={filteredOut} />
              ) : (
                <span className="rail-empty">
                  {out.length ? "no matching links" : "no outbound links"}
                </span>
              )}
            </div>
          </section>

          <section id="rail-in">
            <Label>Backlinks</Label>
            <div id="rail-in-content">
              <Backlinks
                links={filteredBacklinks}
                orphan={rail.orphan}
                filtered={Boolean(query || kind)}
              />
            </div>
          </section>
        </Tabs.Content>

        {/* Cytoscape measures its container once, at construction, and does
            not re-fit on its own — mounting MiniGraph inside a hidden panel
            lays the graph out for a 0-height box. Gating the mount on the
            selected tab (Ark renders inactive panels, it does not skip them)
            means it only ever measures a visible container. */}
        <Tabs.Content value="graph" className="rail-panel rail-panel-graph">
          {tab === "graph" ? (
            <MiniGraph graphDataURL={rail.graphDataURL} focusNode={relpath} />
          ) : null}
        </Tabs.Content>
      </Tabs.Root>
    </aside>
  );
}
