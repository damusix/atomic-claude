// MiniGraph — the rail's this-page neighbor graph, ported from
// templates/layout.html's mountRailGraph(): lazy-loads the carried Cytoscape
// vendor script, fetches graphDataURL (the carried /graph/data endpoint),
// mounts with railCytoscapeStyle, and wires hover/click through
// utils/graphUI (the shared AtomicGraphUI contract) — hover shows the
// preview card, click navigates to the neighbor.
import { useEffect, useRef } from "react";
import { attempt } from "@logosdx/utils";
import { api } from "../../utils/api";
import { hidePreviewCard, navigateToPage, showPreviewCard } from "../../utils/graphUI";
import { loadScript } from "../../utils/loadScript";
import { railCytoscapeStyle } from "./railCytoscapeStyle";

// Minimal shape of the carried Cytoscape instance this component touches —
// the library itself stays a vanilla global (see frontend/CLAUDE.md's
// "carried code" section), so this is a structural interface, not an import.
interface CyNode {
  data(key: string): unknown;
  renderedPosition(): { x: number; y: number };
}
interface CyInstance {
  resize(): void;
  fit(elements: undefined, padding: number): void;
  one(event: string, cb: () => void): void;
  ready(cb: () => void): void;
  on(event: string, selector: string, cb: (evt: { target: CyNode }) => void): void;
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

  useEffect(() => {
    let cancelled = false;
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

        const cy = window.cytoscape({
          container,
          elements,
          layout: {
            name: "concentric",
            concentric: (n: CyNode) => (n.data("id") === focusNode ? 2 : 1),
            levelWidth: () => 1,
            minNodeSpacing: 20,
          },
          style: railCytoscapeStyle(focusNode),
        });

        cy.one("layoutstop", () => {
          cy.resize();
          cy.fit(undefined, 12);
        });
        cy.ready(() => cy.resize());

        cy.on("mouseover", "node", (evt) => {
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
          );
        });
        cy.on("mouseout", "node", () => hidePreviewCard());
        cy.on("tap", "node", (evt) => navigateToPage(evt.target.data("id") as string));
      });

    return () => {
      cancelled = true;
    };
    // Remount on either the focused page or its graph data source changing —
    // navigating to a different page needs a fresh mount, not a patched one.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graphDataURL, focusNode]);

  return <div ref={containerRef} data-rail-graph-url={graphDataURL} data-focus-node={focusNode} />;
}
