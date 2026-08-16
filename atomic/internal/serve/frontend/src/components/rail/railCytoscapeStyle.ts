// railCytoscapeStyle — atomicCyStyle() (templates/layout.html) ported for the
// rail mini-graph, built from utils/typeColors (the sole color source — no
// re-derived color map here). The system-graph/code-graph carried profiles
// (CP8) build their own point/link styling directly against the cosmos.gl
// engine; this factory is Cytoscape-only, rail-only, matching the original's
// "do not copy the style array elsewhere" note.
import { darken, graphTypeColors, type TypeColorMap } from "../../utils/typeColors";

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

export const TYPE_NAMES = [
  "page",
  "repo",
  "concern",
  "knowledge",
  "bucket",
  "external",
  "index",
  "domain",
];

// Type -> Cytoscape shape, mirroring TYPE_SHAPE in utils/typeColors (which is
// cosmos's numeric enum). The two renderers name their shapes differently and
// their vocabularies are not identical, so `external` — a cross in the full
// graph — takes the nearest mark Cytoscape has. `tag` (a rectangle pointing
// off to one side) rather than `vee`: a bare chevron read as a stray
// arrowhead, especially now that edges have none.
export const CY_SHAPE: Record<string, string> = {
  page: "ellipse",
  repo: "hexagon",
  domain: "rectangle",
  concern: "triangle",
  knowledge: "star",
  bucket: "pentagon",
  index: "diamond",
  external: "tag",
};

// Edge colour by direction relative to the focused page — the question this
// panel exists to answer ("what points here, what does this point at"), which
// an arrowhead alone answers badly at 8px. Deliberately outside the node hue
// vocabulary's role assignments: these read as line colours, not as types.
// Chosen by measured perceptual distance, not by eye: the previous pale set
// put incoming and both-ways only ~42 ΔE apart, which is fine for a swatch and
// not for a 1px line. These sit at least 73 ΔE from each other AND clear of
// every node fill, so an edge never reads as a node. Amber scores higher still
// but lands 6 ΔE from `page`, and near-white would disappear in light theme.
export const DIR_COLOR = {
  out: "#38bdf8",
  in: "#f43f5e",
  both: "#e879f9",
  // Neighbour-to-neighbour links, which touch the focus at neither end.
  none: "#6b6255",
} as const;

export const DIR_LABEL: Record<string, string> = {
  out: "outgoing",
  in: "incoming",
  both: "both ways",
};

function colorStr(colors: TypeColorMap, key: string, fallback: string): string {
  const v = colors[key];
  return typeof v === "string" && v ? v : fallback;
}

// railCytoscapeStyle returns the full node/edge stylesheet, then overrides
// the default node entry with the compact 10px rail dot and highlights
// focusNode with the amber selected gradient — the same two overrides
// mountRailGraph (layout.html) applied on top of atomicCyStyle().
export function railCytoscapeStyle(focusNode: string): CyStyleRule[] {
  // The same palette the full-page graph paints with, so a type is one colour
  // across the app rather than dusky here and vivid there.
  const colors = graphTypeColors();

  const typeSelectors: CyStyleRule[] = TYPE_NAMES.map((t) => {
    const base = colorStr(colors, t, colorStr(colors, "default-fill", "#c9b892"));
    const darker = darken(base, 0.18);
    return {
      selector: `node[type="${t}"]`,
      style: {
        "background-fill": "linear-gradient",
        "background-gradient-direction": "to-bottom-right",
        "background-gradient-stop-colors": `${base} ${darker}`,
        "background-gradient-stop-positions": "0% 100%",
        "border-color": darker,
        "shadow-color": base,
        // Shape carries the type too, matching the full graph — see
        // CY_SHAPE for why the vocabularies differ.
        shape: CY_SHAPE[t] ?? "ellipse",
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
        // Labels are carried but not drawn. In a 300px rail every label is
        // wider than the gap between the nodes it sits between, so drawing
        // them all buried the shape of the graph under a wall of overlapping
        // chips — the thing the panel exists to show. MiniGraph adds the
        // `hovered` class on pointer-over, which is when a name is actually
        // being asked for.
        "text-opacity": 0,
      },
    },
    {
      selector: "node.hovered",
      style: {
        "text-opacity": 1,
        // Above its neighbours, so the one label on screen is never clipped
        // by a node drawn after it.
        "z-index": 99,
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
      // Base edge: 1px, like the full graph. No arrowhead — colour already
      // says which way a link runs, and at this scale a triangle is a blob
      // that costs more pixels than the line it decorates. Kind (md-link vs
      // wikilink) stays in the line style; DIRECTION relative to the focused
      // page is the colour, since that is the question this panel answers.
      selector: "edge",
      style: {
        "line-color": DIR_COLOR.none,
        "target-arrow-shape": "none",
        "curve-style": "bezier",
        width: 1,
        opacity: 0.55,
      },
    },
    { selector: "edge.md-link", style: { "line-style": "solid" } },
    { selector: "edge.wikilink", style: { "line-style": "dashed" } },
    {
      // Leaving the focused page.
      selector: "edge.dir-out",
      style: {
        "line-color": DIR_COLOR.out,
        opacity: 1,
      },
    },
    {
      // Pointing at the focused page.
      selector: "edge.dir-in",
      style: {
        "line-color": DIR_COLOR.in,
        opacity: 1,
      },
    },
    {
      // Linked both ways.
      selector: "edge.dir-both",
      style: {
        "line-color": DIR_COLOR.both,
        opacity: 1,
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
