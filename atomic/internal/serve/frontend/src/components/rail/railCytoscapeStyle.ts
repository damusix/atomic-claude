// railCytoscapeStyle — atomicCyStyle() (templates/layout.html) ported for the
// rail mini-graph, built from utils/typeColors (the sole color source — no
// re-derived color map here). The system-graph/code-graph carried profiles
// (CP8) build their own point/link styling directly against the cosmos.gl
// engine; this factory is Cytoscape-only, rail-only, matching the original's
// "do not copy the style array elsewhere" note.
import { atomicCyTypeColors, darken, type TypeColorMap } from "../../utils/typeColors";

// Cytoscape's own stylesheet type isn't imported (the library is carried as
// a vanilla global, not a bundled dependency) — a style rule is a plain
// {selector, style} object of string-keyed CSS-like properties.
export interface CyStyleRule {
  selector: string;
  style: Record<string, string | number>;
}

// A minimal structural interface for the retheme cascade (CP11) — the same
// pattern MiniGraph.tsx uses for its own CyInstance, scoped to just the one
// method restyle needs.
interface RestyleableCy {
  style(rules: CyStyleRule[]): void;
}

declare global {
  interface Window {
    // Set by MiniGraph on mount (registerRailCy) so the theme-toggle retheme
    // cascade can re-apply the stylesheet without MiniGraph itself
    // subscribing to "theme.changed" — ports templates/layout.html's
    // `window.__railCy.style(atomicCyStyle())` re-application on toggle.
    __railCy?: RestyleableCy;
  }
}

let currentFocusNode = "";

// registerRailCy stashes the live Cytoscape instance and its focus node so
// rethemeRailGraph can rebuild and re-apply the stylesheet later. Called by
// MiniGraph after each (re)mount; a stale registration from an unmounted
// instance is harmless — the next mount overwrites it, and restyling a
// detached Cytoscape instance is a documented no-op in Cytoscape itself.
export function registerRailCy(cy: RestyleableCy, focusNode: string): void {
  window.__railCy = cy;
  currentFocusNode = focusNode;
}

// rethemeRailGraph re-applies railCytoscapeStyle (freshly rebuilt from the
// now-flipped CSS vars) to the currently registered instance. A no-op with
// nothing mounted (page view with no rail graph, or theme toggled before any
// MiniGraph mount completed).
export function rethemeRailGraph(): void {
  window.__railCy?.style(railCytoscapeStyle(currentFocusNode));
}

const TYPE_NAMES = ["page", "repo", "concern", "knowledge", "bucket", "external", "index", "domain"];

function colorStr(colors: TypeColorMap, key: string, fallback: string): string {
  const v = colors[key];
  return typeof v === "string" && v ? v : fallback;
}

// railCytoscapeStyle returns the full node/edge stylesheet, then overrides
// the default node entry with the compact 10px rail dot and highlights
// focusNode with the amber selected gradient — the same two overrides
// mountRailGraph (layout.html) applied on top of atomicCyStyle().
export function railCytoscapeStyle(focusNode: string): CyStyleRule[] {
  const colors = atomicCyTypeColors();

  const typeSelectors: CyStyleRule[] = TYPE_NAMES.map((t) => {
    const base = colorStr(colors, t, colorStr(colors, "default-fill", "#c9b892"));
    const darker = colorStr(colors, `${t}-dark`, darken(base, 0.1));
    return {
      selector: `node[type="${t}"]`,
      style: {
        "background-fill": "linear-gradient",
        "background-gradient-direction": "to-bottom-right",
        "background-gradient-stop-colors": `${base} ${darker}`,
        "background-gradient-stop-positions": "0% 100%",
        "border-color": darker,
        "shadow-color": base,
        color: colorStr(colors, `${t}-ink`, colorStr(colors, "default-label", "#211d18")),
      },
    };
  });

  const defaultFill = colorStr(colors, "default-fill", "#c9b892");
  const defaultDark = colorStr(colors, "default-dark", darken(defaultFill, 0.1));
  const defaultLabel = colorStr(colors, "default-label", "#211d18");
  const labelBg = colorStr(colors, "label-bg", "#ffffff");
  const labelBorder = colorStr(colors, "label-border", "#e2ddd2");
  const selColor = colorStr(colors, "selected", "#d99a1f");
  const selDark = darken(selColor, 0.1);

  const rules: CyStyleRule[] = [
    {
      // Compact rail node: 10px gradient dot, capped size (no degree-based
      // scaling — the system graph's mapData(deg,…) sizing is a full-graph
      // concern the rail's this-page neighbor map doesn't need).
      selector: "node",
      style: {
        label: "data(label)",
        "background-fill": "linear-gradient",
        "background-gradient-direction": "to-bottom-right",
        "background-gradient-stop-colors": `${defaultFill} ${defaultDark}`,
        "background-gradient-stop-positions": "0% 100%",
        "border-width": "1px",
        "border-color": defaultDark,
        color: defaultLabel,
        "font-family": '"JetBrains Mono", "Geist Mono", ui-monospace, monospace',
        "font-size": "9px",
        "text-valign": "bottom",
        "text-halign": "center",
        "text-margin-y": "5px",
        width: "10px",
        height: "10px",
        shape: "ellipse",
        "text-max-width": "74px",
        "text-wrap": "ellipsis",
        "text-background-color": labelBg,
        "text-background-opacity": 0.94,
        "text-background-padding": "3px",
        "text-background-shape": "roundrectangle",
        "text-border-color": labelBorder,
        "text-border-width": 1,
        "text-border-opacity": 0.9,
        "shadow-blur": "5px",
        "shadow-color": colorStr(colors, "page", defaultFill),
        "shadow-opacity": 0.12,
        "shadow-offset-x": 0,
        "shadow-offset-y": 0,
      },
    },
    {
      selector: `node[id="${focusNode}"]`,
      style: {
        "background-fill": "linear-gradient",
        "background-gradient-direction": "to-bottom-right",
        "background-gradient-stop-colors": `${selColor} ${selDark}`,
        "background-gradient-stop-positions": "0% 100%",
        "border-color": selDark,
        "shadow-color": selColor,
        "shadow-opacity": 0.35,
      },
    },
    ...typeSelectors,
    {
      selector: "edge.md-link",
      style: {
        "line-color": colorStr(colors, "edge", "#cabfae"),
        "line-style": "solid",
        "target-arrow-color": colorStr(colors, "edge", "#cabfae"),
        "target-arrow-shape": "triangle",
        "curve-style": "bezier",
        width: 1.5,
        "arrow-scale": 0.9,
      },
    },
    {
      selector: "edge.wikilink",
      style: {
        "line-color": colorStr(colors, "edge-strong", "#b1a48f"),
        "line-style": "dashed",
        "target-arrow-color": colorStr(colors, "edge-strong", "#b1a48f"),
        "target-arrow-shape": "triangle",
        "curve-style": "bezier",
        width: 1.5,
        "arrow-scale": 0.9,
      },
    },
    {
      selector: ":selected",
      style: {
        "background-fill": "linear-gradient",
        "background-gradient-direction": "to-bottom-right",
        "background-gradient-stop-colors": `${selColor} ${selDark}`,
        "background-gradient-stop-positions": "0% 100%",
        "border-color": selDark,
        "shadow-color": selColor,
        "shadow-opacity": 0.35,
        "line-color": selColor,
      },
    },
  ];

  return rules;
}
