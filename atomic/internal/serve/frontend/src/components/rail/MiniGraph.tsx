// MiniGraph — the rail's this-page neighbor graph, ported from
// templates/layout.html's mountRailGraph(): lazy-loads the carried Cytoscape
// vendor script, fetches graphDataURL (the carried /graph/data endpoint),
// mounts with railCytoscapeStyle, and wires hover/click through
// utils/graphUI (the shared AtomicGraphUI contract) — hover shows the
// preview card, click navigates to the neighbor.
import { useEffect, useRef, useState } from "react";
import { attempt } from "@logosdx/utils";
import { api } from "../../utils/api";
import { hidePreviewCard, navigateToPage, showPreviewCard } from "../../utils/graphUI";
import { loadScript } from "../../utils/loadScript";
import { MiniGraphLegend } from "./MiniGraphLegend";
import { type CyStyleRule, railCytoscapeStyle, registerRailCy } from "./railCytoscapeStyle";

interface EdgeEnds {
  source: string;
  target: string;
}

function edgeEnds(edge: unknown): EdgeEnds | null {
  if (!edge || typeof edge !== "object" || !("data" in edge)) return null;
  const { data } = edge;
  if (!data || typeof data !== "object") return null;
  if (!("source" in data) || !("target" in data)) return null;
  const { source, target } = data;
  if (typeof source !== "string" || typeof target !== "string") return null;
  return { source, target };
}

/**
 * Tags every edge touching `focus` with its direction, so the stylesheet can
 * colour it: dir-out, dir-in, or dir-both when the two pages link each other.
 *
 * Done here rather than server-side because direction is only meaningful
 * relative to the page being viewed — the same edge is outgoing on one page
 * and incoming on the other, so it is not a property of the edge.
 */
function classifyEdgeDirections(elements: unknown, focus: string): unknown {
  if (!elements || typeof elements !== "object" || !("edges" in elements)) return elements;
  const { edges } = elements;
  if (!Array.isArray(edges)) return elements;

  // A neighbour linked in both directions gets both of its edges marked, so
  // the pair reads as one reciprocal relationship rather than two rival ones.
  const out = new Set<string>();
  const into = new Set<string>();
  for (const edge of edges) {
    const ends = edgeEnds(edge);
    if (!ends) continue;
    if (ends.source === focus) out.add(ends.target);
    if (ends.target === focus) into.add(ends.source);
  }

  for (const edge of edges) {
    const ends = edgeEnds(edge);
    if (!ends || !edge || typeof edge !== "object") continue;
    const other = ends.source === focus ? ends.target : ends.target === focus ? ends.source : null;
    if (other === null) continue;
    const dir = out.has(other) && into.has(other) ? "both" : ends.source === focus ? "out" : "in";
    const existing = "classes" in edge && typeof edge.classes === "string" ? edge.classes : "";
    Object.assign(edge, { classes: `${existing} dir-${dir}`.trim() });
  }
  return elements;
}

// The types actually present in this neighbourhood, so the legend describes
// what is on screen rather than the full taxonomy.
function collectTypes(elements: unknown): string[] {
  if (!elements || typeof elements !== "object" || !("nodes" in elements)) return [];
  const { nodes } = elements;
  if (!Array.isArray(nodes)) return [];

  const seen = new Set<string>();
  for (const node of nodes) {
    if (!node || typeof node !== "object" || !("data" in node)) continue;
    const { data } = node;
    if (!data || typeof data !== "object" || !("type" in data)) continue;
    const { type } = data;
    if (typeof type === "string" && type) seen.add(type);
  }
  return [...seen].sort();
}

// Minimal shape of the carried Cytoscape instance this component touches —
// the library itself stays a vanilla global (see frontend/CLAUDE.md's
// "carried code" section), so this is a structural interface, not an import.
interface CyNode {
  data(key: string): unknown;
  renderedPosition(): { x: number; y: number };
  addClass(name: string): void;
  removeClass(name: string): void;
}
interface CyInstance {
  resize(): void;
  destroy(): void;
  fit(elements: undefined, padding: number): void;
  one(event: string, cb: () => void): void;
  ready(cb: () => void): void;
  on(event: string, selector: string, cb: (evt: { target: CyNode }) => void): void;
  // Selector-less form: the event target is the instance itself when the tap
  // landed on empty canvas rather than an element.
  on(event: string, cb: (evt: { target: unknown }) => void): void;
  style(rules: CyStyleRule[]): void;
}
type CytoscapeFactory = (opts: {
  container: HTMLElement;
  elements: unknown;
  layout: Record<string, unknown>;
  style: unknown;
}) => CyInstance;

declare global {
  interface Window {
    cytoscape?: CytoscapeFactory;
  }
}

export function MiniGraph({ graphDataURL, focusNode }: { graphDataURL: string; focusNode: string }) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [types, setTypes] = useState<string[]>([]);

  useEffect(() => {
    let cancelled = false;
    let instance: CyInstance | null = null;
    const container = containerRef.current;
    if (!container) return;

    // graphDataURL is the carried, non-/api/* /graph/data endpoint (unchanged
    // path per the API contracts conventions) — the one shared FetchEngine
    // instance is fixed to baseUrl /api, but its executor special-cases any
    // path starting with "http" to bypass baseUrl entirely (no second engine
    // needed, per frontend/CLAUDE.md's "rules on the one engine" rule), so
    // resolving graphDataURL against location.origin routes it through the
    // same api.get() every other component uses.
    const absoluteGraphDataURL = new URL(graphDataURL, window.location.origin).toString();
    void attempt(() => loadScript("/vendor/cytoscape.min.js"))
      .then(([, loadErr]): Promise<[unknown, Error | null]> => {
        if (loadErr) return Promise.resolve([null, loadErr]);
        return attempt(async () => {
          const res = await api.get<unknown>(absoluteGraphDataURL);
          if (!res.ok) throw new Error(`graph data request failed: ${res.status}`);
          return res.data;
        });
      })
      .then(([elements, err]) => {
        if (cancelled || err || !window.cytoscape || !container) return;

        setTypes(collectTypes(elements));

        const cy = (instance = window.cytoscape({
          container,
          elements: classifyEdgeDirections(elements, focusNode),
          // Force-directed, not concentric. Concentric put every non-focus
          // node on one ring, so the neighbourhood's shape was a circle
          // whatever the links actually were and every relationship crossed
          // the middle as a chord. cose is Cytoscape's own force layout — the
          // clustered, organic arrangement cola gives, without vendoring cola
          // and webcola for one panel.
          //
          // The trade is that the focus node no longer sits at the centre by
          // construction; it stays identifiable through railCytoscapeStyle,
          // which draws it differently from its neighbours.
          layout: {
            name: "cose",
            // The rail is ~300px wide and the graph is re-laid on every page
            // change: an animated settle is motion the reader did not ask for.
            animate: false,
            // Tuned for a narrow column — the defaults assume a full canvas
            // and push disconnected pieces far enough apart that fit() has to
            // zoom out until the labels are unreadable.
            idealEdgeLength: () => 45,
            nodeRepulsion: () => 6000,
            componentSpacing: 30,
            gravity: 60,
            nestingFactor: 1.2,
            numIter: 1200,
            padding: 8,
          },
          style: railCytoscapeStyle(focusNode),
        }));

        cy.one("layoutstop", () => {
          cy.resize();
          cy.fit(undefined, 12);
        });
        cy.ready(() => cy.resize());

        // Hover reveals the label and nothing else. Showing the card here too
        // put two things under the pointer at once, each covering the other,
        // and the card's own contents were unreachable because leaving the
        // node dismissed it.
        cy.on("mouseover", "node", (evt) => {
          evt.target.addClass("hovered");
        });
        cy.on("mouseout", "node", (evt) => {
          evt.target.removeClass("hovered");
        });

        // Click opens the card, which now carries the Open button that used to
        // be this click's job — so reading a node no longer means committing
        // to navigating away from the page you are on.
        cy.on("tap", "node", (evt) => {
          const n = evt.target;
          showPreviewCard(
            {
              type: n.data("type") as string | undefined,
              title: n.data("title") as string | undefined,
              label: n.data("label") as string | undefined,
              description: n.data("description") as string | undefined,
              snippet: n.data("snippet") as string | undefined,
            },
            n.renderedPosition(),
            container,
            n.data("id") as string,
          );
        });
        // Tapping the canvas itself dismisses it — the card is sticky now, so
        // something has to put it away.
        cy.on("tap", (evt) => {
          if (evt.target === cy) hidePreviewCard();
        });

        // Expose for the theme-toggle retheme cascade (CP11) — see
        // railCytoscapeStyle's registerRailCy comment.
        registerRailCy(cy, focusNode);
      });

    // The graph now mounts and unmounts every time its rail tab is selected,
    // so an undestroyed instance is a per-switch leak of a canvas plus its
    // event listeners, not a once-per-session one.
    return () => {
      cancelled = true;
      instance?.destroy();
      instance = null;
    };
    // Remount on either the focused page or its graph data source changing —
    // navigating to a different page needs a fresh mount, not a patched one.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graphDataURL, focusNode]);

  return (
    <>
      <div ref={containerRef} data-rail-graph-url={graphDataURL} data-focus-node={focusNode} />
      <MiniGraphLegend types={types} />
    </>
  );
}
