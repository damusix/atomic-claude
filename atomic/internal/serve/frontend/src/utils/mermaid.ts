// utils/mermaid — mounts fenced ```mermaid blocks (server-emitted as
// <pre class="mermaid">…raw…</pre>, see render.go's mermaidCodeRenderer)
// within a container. Ports templates/layout.html's atomicMermaidInit()/
// atomicMermaidThemeVars()/atomicMermaidRetheme() — initial mount
// (mountMermaid, CP6) plus the light/dark retheme re-run (rethemeMermaid,
// CP11's useTheme retheme cascade).
import { attempt, attemptSync } from "@logosdx/utils";
import { loadScript } from "./loadScript";
import { registerElkLayouts } from "./mermaidElk";
import { usesElk, withElk } from "./mermaidLayout";
import { chipLabels, makeRoomForPadding } from "./mermaidSvg";

interface MermaidLike {
  initialize(config: Record<string, unknown>): void;
  run(opts?: { nodes: ArrayLike<HTMLElement> }): void | Promise<void>;
  registerLayoutLoaders(loaders: unknown[]): void;
  detectType(text: string): string;
}

declare global {
  interface Window {
    mermaid?: MermaidLike;
  }
}

function readVar(style: CSSStyleDeclaration, name: string, fallback: string): string {
  return style.getPropertyValue(name).trim() || fallback;
}

// The eight ramp hues, in the order diagrams should cycle them.
//
// Shade 1, the lightest, is what these fills use. A mid tone is the hostile
// case for a filled shape carrying a label: on light-theme slate-2 white text
// measures 4.5:1 and dark text 3.7:1, so neither is comfortably legible and no
// single label color works across the eight hues. Every shade 1 clears 5:1
// against dark ink in both themes, which is what lets one label color be right
// everywhere — including the diagrams that ignore the label variables and take
// the page ink instead.
const RAMP_HUES = ["gold", "slate", "moss", "plum", "magenta", "terra", "cyan", "gray"] as const;

const CATEGORICAL_SHADE = 1;

// Only reached when the stylesheet has not loaded; these mirror the light
// theme's shade 1.
const RAMP_FALLBACK: Record<string, string> = {
  gold: "#e3b84d",
  slate: "#7d94bd",
  moss: "#99a355",
  plum: "#a57dc0",
  magenta: "#bf6a8b",
  terra: "#d17f42",
  cyan: "#62a8ab",
  gray: "#a89f8d",
};

function rampSeries(s: CSSStyleDeclaration, shade: number): string[] {
  return RAMP_HUES.map((hue) =>
    readVar(s, `--ramp-${hue}-${shade}`, RAMP_FALLBACK[hue] ?? "#8a8171"),
  );
}

// Mirrors app.css's --ink-on-fill, which also colors the pre.mermaid boundary
// so the families that paint no label of their own inherit a legible one.
const INK_ON_FILL_FALLBACK = "#1a1712";
const PAPER_ON_FILL = "#ffffff";

// Fills cycled across every `.node`, plus sequence actors and notes. Theme-
// independent: each takes a black label and border; only the canvas changes.
const NODE_FILLS = ["#ff6b6b", "#feca57", "#48dbfb", "#ff9ff3", "#1dd1a1"] as const;
const NODE_INK = "#000000";

function readCanvas(s: CSSStyleDeclaration): string {
  return readVar(s, "--diagram-canvas", "#fff6e5");
}

function relativeLuminance(hex: string): number {
  const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
  if (!m || !m[1]) return 0.5;
  const raw = m[1];
  const channels = [0, 2, 4].map((i) => {
    const v = Number.parseInt(raw.slice(i, i + 2), 16) / 255;
    return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * (channels[0] ?? 0) + 0.7152 * (channels[1] ?? 0) + 0.0722 * (channels[2] ?? 0);
}

// Label color for text sitting on a filled shape. Whichever of ink or paper
// actually contrasts with this fill — a fixed choice is legible on one shade
// and invisible on another, and the ramp is not uniform across themes.
function labelOn(s: CSSStyleDeclaration, fill: string): string {
  const ink = readVar(s, "--ink-on-fill", INK_ON_FILL_FALLBACK);
  const l = relativeLuminance(fill);
  const withInk = (l + 0.05) / (relativeLuminance(ink) + 0.05);
  const withPaper = (relativeLuminance(PAPER_ON_FILL) + 0.05) / (l + 0.05);
  return withInk >= withPaper ? ink : PAPER_ON_FILL;
}

// Mermaid keeps a separate palette variable family per diagram type, and every
// family it is not given falls back on its own: the categorical ones
// (pie1-12, git0-7, cScale0-11) derive from primaryColor, so every slice and
// branch comes out the same tone; the fixed ones (ER attribute rows, gantt crit bars, sankey links) fall
// back to defaults that ignore the theme — white rows under light-on-dark ink,
// a pure #ff0000 bar. Naming each family is what keeps a diagram inside the
// palette. Indexed entries are expanded from the ramp below.
function mermaidThemeVars(): Record<string, unknown> {
  const s = getComputedStyle(document.documentElement);
  const ink = readVar(s, "--ink", "#211d18");
  const inkSoft = readVar(s, "--ink-soft", "#57514a");
  const surface = readVar(s, "--paper-raised", "#ffffff");
  const sunk = readVar(s, "--paper-sunk", "#f5f3ee");
  const paper = readVar(s, "--paper", "#fbfaf8");
  const border = readVar(s, "--hairline-2", "#e2ddd2");
  const line = readVar(s, "--edge-strong", "#b1a48f");
  const sans = readVar(s, "--sans", '"Inter", system-ui, sans-serif');
  const amber = readVar(s, "--amber", "#b07d18");
  const amberEdge = readVar(s, "--amber-edge", "#ecd9aa");
  const canvas = readCanvas(s);
  const diagramLine = readVar(s, "--diagram-line", line);
  const diagramLabel = readVar(s, "--diagram-label", ink);
  const [fill0, fill1, fill2, fill3] = NODE_FILLS;

  const series = rampSeries(s, CATEGORICAL_SHADE);
  // Whatever contrasts with the ramp in this theme. Single-valued label
  // variables cover a whole family at once, so they take the reading for the
  // first hue; the indexed families below are resolved per fill.
  const onFill = labelOn(s, series[0] ?? "#8a8171");

  const vars: Record<string, unknown> = {
    background: paper,
    mainBkg: fill0,
    primaryColor: fill0,
    primaryTextColor: NODE_INK,
    primaryBorderColor: NODE_INK,
    secondaryColor: sunk,
    secondaryTextColor: ink,
    secondaryBorderColor: border,
    tertiaryColor: paper,
    tertiaryTextColor: ink,
    tertiaryBorderColor: border,
    nodeBorder: NODE_INK,
    nodeTextColor: NODE_INK,
    lineColor: diagramLine,
    textColor: ink,
    titleColor: ink,
    edgeLabelBackground: canvas,
    clusterBkg: canvas,
    clusterBorder: NODE_INK,
    fontFamily: sans,
    fontSize: "14px",

    // State.
    transitionColor: diagramLine,
    transitionLabelColor: diagramLabel,
    stateLabelColor: NODE_INK,
    labelBackgroundColor: canvas,
    specialStateColor: fill0,

    // No ER row variables: mermaid derives row fills from `background`, which puts
    // the black labels on near-black in dark mode. nodeStyleCSS sets them light.

    // Sequence.
    actorBkg: fill1,
    actorBorder: NODE_INK,
    actorTextColor: NODE_INK,
    actorLineColor: diagramLine,
    signalColor: diagramLine,
    signalTextColor: ink,
    labelBoxBkgColor: fill0,
    labelBoxBorderColor: NODE_INK,
    labelTextColor: NODE_INK,
    loopTextColor: ink,
    noteBkgColor: fill2,
    noteBorderColor: NODE_INK,
    noteTextColor: NODE_INK,
    activationBkgColor: fill3,
    activationBorderColor: NODE_INK,
    sequenceNumberColor: onFill,

    // Gantt.
    sectionBkgColor: sunk,
    altSectionBkgColor: canvas,
    sectionBkgColor2: sunk,
    gridColor: border,
    taskBkgColor: series[1],
    taskBorderColor: border,
    // Text on a bar takes the fill's reading; text beside a bar sits on the
    // page and takes the page's ink. Mermaid's "dark"/"light" here name the
    // text, not the surface.
    taskTextColor: onFill,
    taskTextLightColor: onFill,
    taskTextDarkColor: onFill,
    taskTextOutsideColor: ink,
    activeTaskBkgColor: amber,
    activeTaskBorderColor: amber,
    doneTaskBkgColor: readVar(s, "--ink-ghost", "#b4ada3"),
    doneTaskBorderColor: border,
    critBkgColor: series[4],
    critBorderColor: series[4],
    todayLineColor: amber,

    // Pie and quadrant.
    pieTitleTextColor: ink,
    pieSectionTextColor: onFill,
    pieLegendTextColor: ink,
    pieStrokeColor: paper,
    pieOuterStrokeColor: border,
    quadrant1Fill: surface,
    quadrant2Fill: sunk,
    quadrant3Fill: surface,
    quadrant4Fill: sunk,
    quadrant1TextFill: ink,
    quadrant2TextFill: ink,
    quadrant3TextFill: ink,
    quadrant4TextFill: ink,
    quadrantPointFill: series[0],
    quadrantPointTextFill: ink,
    quadrantXAxisTextFill: inkSoft,
    quadrantYAxisTextFill: inkSoft,
    quadrantInternalBorderStrokeFill: border,
    quadrantExternalBorderStrokeFill: border,
    quadrantTitleFill: ink,

    // Git.
    commitLabelColor: ink,
    commitLabelBackground: sunk,
    tagLabelColor: ink,
    tagLabelBackground: readVar(s, "--amber-wash", "#fbf3e0"),
    tagLabelBorder: amberEdge,

    // xychart and radar read nested objects rather than flat keys, and both
    // otherwise fall back to a stock palette that ignores the theme.
    xyChart: {
      backgroundColor: canvas,
      titleColor: ink,
      xAxisLabelColor: ink,
      xAxisTitleColor: ink,
      xAxisTickColor: border,
      xAxisLineColor: border,
      yAxisLabelColor: ink,
      yAxisTitleColor: ink,
      yAxisTickColor: border,
      yAxisLineColor: border,
      plotColorPalette: series.join(","),
    },
    radar: {
      axisColor: border,
      axisStrokeWidth: 1,
      axisLabelFontSize: 12,
      curveOpacity: 0.4,
      curveStrokeWidth: 2,
      graticuleColor: border,
      graticuleOpacity: 0.5,
      graticuleStrokeWidth: 1,
      legendBoxSize: 12,
      legendFontSize: 12,
    },
  };

  // Categorical families. Mermaid indexes these by position, so each is the
  // ramp cycled to the length that family expects, and each label is resolved
  // against the fill it actually lands on.
  series.forEach((color, i) => {
    vars[`git${i}`] = color;
    vars[`gitBranchLabel${i}`] = labelOn(s, color);
    vars[`fillType${i}`] = color;
  });
  for (let i = 0; i < 12; i += 1) {
    const color = series[i % series.length] ?? series[0] ?? "#8a8171";
    vars[`pie${i + 1}`] = color;
    vars[`cScale${i}`] = color;
    vars[`cScaleLabel${i}`] = labelOn(s, color);
    vars[`cScaleInv${i}`] = labelOn(s, color);
  }

  return vars;
}

// Every node outline: path covers the hand-drawn shapes (cylinder, stadium, class
// and ER boxes). The state-start dot, ER rows and class dividers keep their own.
const NODE_SHAPE =
  ":is(rect, polygon, path, circle):not(.state-start):not(:is(.row-rect-odd, .row-rect-even, .divider) *)";

// Styles mermaid has no theme variable for. Mermaid scopes this under each
// diagram's svg id, so the canvas behind the diagram lives in app.css instead.
function nodeStyleCSS(): string {
  const s = getComputedStyle(document.documentElement);
  const line = readVar(s, "--diagram-line", NODE_INK);
  const label = readVar(s, "--diagram-label", NODE_INK);
  const shadow = readVar(s, "--diagram-shadow", "rgba(0, 0, 0, 0.35)");
  const cycle = NODE_FILLS.map(
    (fill, i) => `.node:nth-child(${NODE_FILLS.length}n+${i + 1}) ${NODE_SHAPE} { fill: ${fill}; }`,
  ).join("\n");
  return `
    .node ${NODE_SHAPE}, rect.actor, rect.note, .cluster rect, .labelBox {
      stroke-width: 3px;
      rx: 8px;
      ry: 8px;
      filter: drop-shadow(3px 5px 8px ${shadow});
    }
    ${cycle}
    .nodeLabel { font-weight: 600; color: ${NODE_INK}; }
    .state-start { stroke: ${NODE_INK} !important; stroke-width: 3px; filter: drop-shadow(2px 3px 5px ${shadow}); }
    .flowchart-link, .edgePath > path, .transition, .messageLine0, .messageLine1 { stroke-width: 2.5px; }
    .row-rect-odd > path { fill: #ffffff; }
    .row-rect-even > path { fill: #f3efe6; }
    .actor-line { stroke-width: 2px; stroke-dasharray: 6 4; }
    .loopLine { stroke: ${line}; stroke-width: 2px; }
    marker:is([id$="-pointEnd"], [id$="-pointStart"]) { overflow: visible; }
    marker[id$="-pointEnd"] .arrowMarkerPath { transform-origin: 10px 5px; transform: translateX(-1px) scale(1.6); }
    marker[id$="-pointStart"] .arrowMarkerPath { transform-origin: 0 5px; transform: translateX(1px) scale(1.6); }
    .edgeLabel, .edgeLabel p, .labelBkg { background-color: transparent; color: ${label}; font-weight: 600; }
  `;
}

function mermaidConfig(): Record<string, unknown> {
  return {
    startOnLoad: false,
    securityLevel: "loose",
    theme: "base",
    flowchart: { padding: 16, diagramPadding: 20 },
    sequence: { diagramMarginX: 20, diagramMarginY: 20 },
    themeVariables: mermaidThemeVars(),
    themeCSS: nodeStyleCSS(),
  };
}

// Mermaid sizes each node box by measuring its label as it renders. Measured
// in the fallback face before Inter arrives, labels overflow once it swaps in.
async function fontsReady(): Promise<void> {
  if (!document.fonts) return;
  const sans = readVar(getComputedStyle(document.documentElement), "--sans", "Inter, sans-serif");
  await Promise.allSettled(["400", "600"].map((weight) => document.fonts.load(`${weight} 14px ${sans}`)));
}

function setSource(node: HTMLElement, text: string): void {
  node.removeAttribute("data-processed");
  node.textContent = text;
}

// Keyboard path to the viewer; pointer users click the diagram itself. Named
// for its diagram so a list of a page's buttons tells them apart.
function addOpenButton(node: HTMLElement, svg: SVGSVGElement, position: number): void {
  if (node.querySelector(".mermaid-open")) return;
  const title = svg.querySelector(":scope > title")?.textContent?.trim();
  const button = document.createElement("button");
  button.type = "button";
  button.className = "mermaid-open";
  button.textContent = title ? `Open “${title}” in viewer` : `Open diagram ${position} in viewer`;
  node.appendChild(button);
}

function finish(node: HTMLElement, canvas: string, position: number): void {
  const svg = node.querySelector("svg");
  if (!svg) return;
  makeRoomForPadding(svg);
  chipLabels(svg, canvas);
  addOpenButton(node, svg, position);
}

// One node per run: run throws when a diagram fails, and only a per-node throw
// says which one. An ELK failure can leave a half-drawn svg with no error marker.
async function runOne(mermaid: MermaidLike, node: HTMLElement): Promise<boolean> {
  const [, err] = await attempt(async () => mermaid.run({ nodes: [node] }));
  return !err;
}

async function render(mermaid: MermaidLike, nodes: HTMLElement[]): Promise<void> {
  const elkReady = await registerElkLayouts((loaders) => mermaid.registerLayoutLoaders(loaders));
  // Before detectType: initialize is what registers the diagram detectors.
  mermaid.initialize(mermaidConfig());
  const canvas = readCanvas(getComputedStyle(document.documentElement));

  for (const [i, node] of nodes.entries()) {
    const src = node.dataset.mermaidSrc ?? "";
    const [type] = attemptSync(() => mermaid.detectType(src));
    const elk = elkReady && usesElk(type ?? null);
    // ELK routes edges orthogonally with rounded corners.
    setSource(node, elk ? withElk(src) : src);
    let ok = await runOne(mermaid, node);
    if (!ok && elk) {
      setSource(node, src);
      ok = await runOne(mermaid, node);
    }
    // Malformed diagram source keeps mermaid's own error output, unadjusted.
    if (ok) finish(node, canvas, i + 1);
  }
}

// mountMermaid lazy-loads the carried mermaid vendor script (once per page
// load), stashes each diagram's raw source for a later re-render (theme
// toggle), and runs mermaid scoped to this container's <pre.mermaid> nodes
// only — a page swap must not re-render diagrams belonging to a page that's
// no longer mounted.
export async function mountMermaid(container: HTMLElement): Promise<void> {
  const nodes = [...container.querySelectorAll<HTMLElement>("pre.mermaid")];
  if (nodes.length === 0) return;

  const [, loadErr] = await attempt(() => loadScript("/vendor/mermaid.min.js"));
  if (loadErr) {
    // Vendor script unavailable (offline, blocked, test environment with no
    // static server) — leave the raw <pre> visible rather than throwing out
    // of the mount effect.
    return;
  }
  const mermaid = window.mermaid;
  if (!mermaid) return;

  for (const node of nodes) {
    if (!node.dataset.mermaidSrc) node.dataset.mermaidSrc = node.textContent ?? "";
  }

  await fontsReady();
  await render(mermaid, nodes);
}

// rethemeMermaid re-runs every already-mounted diagram in the document
// (data-mermaid-src stashed by mountMermaid) against the now-flipped CSS
// vars. A no-op with nothing mounted or the vendor script never loaded (page
// view with no diagrams, or theme toggled before any mermaid mount completed).
export async function rethemeMermaid(): Promise<void> {
  const mermaid = window.mermaid;
  if (!mermaid) return;
  const nodes = [...document.querySelectorAll<HTMLElement>("pre.mermaid[data-mermaid-src]")];
  if (nodes.length === 0) return;
  await render(mermaid, nodes);
}
