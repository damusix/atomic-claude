// utils/mermaid — mounts fenced ```mermaid blocks (server-emitted as
// <pre class="mermaid">…raw…</pre>, see render.go's mermaidCodeRenderer)
// within a container. Ports templates/layout.html's atomicMermaidInit()/
// atomicMermaidThemeVars()/atomicMermaidRetheme() — initial mount
// (mountMermaid, CP6) plus the light/dark retheme re-run (rethemeMermaid,
// CP11's useTheme retheme cascade).
import { attempt } from "@logosdx/utils";
import { loadScript } from "./loadScript";

interface MermaidLike {
  initialize(config: Record<string, unknown>): void;
  run(opts?: { nodes: NodeListOf<Element> }): void | Promise<void>;
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
// (pie1-12, git0-7, cScale0-11) derive from primaryColor, which is a surface
// color here, so every slice and branch comes out the same near-monochrome
// tone; the fixed ones (ER attribute rows, gantt crit bars, sankey links) fall
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

  const series = rampSeries(s, CATEGORICAL_SHADE);
  // Whatever contrasts with the ramp in this theme. Single-valued label
  // variables cover a whole family at once, so they take the reading for the
  // first hue; the indexed families below are resolved per fill.
  const onFill = labelOn(s, series[0] ?? "#8a8171");

  const vars: Record<string, unknown> = {
    background: paper,
    mainBkg: surface,
    primaryColor: surface,
    primaryTextColor: ink,
    primaryBorderColor: border,
    secondaryColor: sunk,
    secondaryTextColor: ink,
    secondaryBorderColor: border,
    tertiaryColor: paper,
    tertiaryTextColor: ink,
    tertiaryBorderColor: border,
    nodeBorder: border,
    nodeTextColor: ink,
    lineColor: line,
    textColor: ink,
    titleColor: ink,
    edgeLabelBackground: surface,
    clusterBkg: sunk,
    clusterBorder: border,
    fontFamily: sans,
    fontSize: "14px",

    // Entity relationship attribute rows are not here: 11.15 moved ER onto the
    // unified node renderer, which derives the two row fills from `background`
    // by lightness and never reads attributeBackgroundColorOdd/Even. app.css
    // overrides g.row-rect-odd/even instead.

    // Sequence.
    actorBkg: surface,
    actorBorder: border,
    actorTextColor: ink,
    actorLineColor: line,
    signalColor: ink,
    signalTextColor: ink,
    labelBoxBkgColor: sunk,
    labelBoxBorderColor: border,
    labelTextColor: ink,
    loopTextColor: ink,
    noteBkgColor: readVar(s, "--amber-wash", "#fbf3e0"),
    noteBorderColor: amberEdge,
    noteTextColor: ink,
    activationBkgColor: sunk,
    activationBorderColor: border,
    sequenceNumberColor: onFill,

    // Gantt.
    sectionBkgColor: sunk,
    altSectionBkgColor: paper,
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
      backgroundColor: paper,
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

const ZOOM_MIN = 0.2;
const ZOOM_MAX = 8;
// Below this much pointer travel nothing is intercepted, so a click still
// reaches whatever mermaid bound to a node (securityLevel is "loose", so
// diagrams may carry click handlers and links).
const PAN_THRESHOLD_PX = 4;

interface ZoomState {
  x: number;
  y: number;
  k: number;
}

function clamp(value: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, value));
}

// The transform lives as an inline style on the svg, so state and element share
// a fate: a re-render replaces the svg and the transform goes with it. Origin
// is pinned to the top-left so anchoring a zoom on the cursor is plain
// arithmetic rather than an offset correction.
function applyTransform(node: HTMLElement, state: ZoomState): void {
  const svg = node.querySelector<SVGSVGElement>("svg");
  if (!svg) return;
  svg.style.transformOrigin = "0 0";
  svg.style.transform = `translate(${state.x}px, ${state.y}px) scale(${state.k})`;
}

// Identity is also fit-to-screen: :fullscreen caps the svg at 100% of the box
// and centers it, so dropping the transform restores the fitted view.
function clearTransform(node: HTMLElement): void {
  const svg = node.querySelector<SVGSVGElement>("svg");
  if (!svg) return;
  svg.style.removeProperty("transform");
  svg.style.removeProperty("transform-origin");
}

// Pan and zoom, wired only while a diagram is fullscreen. Inline would mean
// taking the wheel away from page scrolling, and swallowing ctrl+wheel there
// would block browser page zoom over any diagram.
//
// Every listener is scoped to one fullscreen session and torn down by one
// AbortController. The <pre> outlives re-renders, so anything left attached to
// it would stack up a set of handlers per theme toggle.
function beginZoomSession(node: HTMLElement): void {
  const controller = new AbortController();
  const { signal } = controller;
  const state: ZoomState = { x: 0, y: 0, k: 1 };

  const reset = (): void => {
    state.x = 0;
    state.y = 0;
    state.k = 1;
    clearTransform(node);
  };

  const zoomAbout = (clientX: number, clientY: number, factor: number): void => {
    const rect = node.getBoundingClientRect();
    const px = clientX - rect.left;
    const py = clientY - rect.top;
    const next = clamp(state.k * factor, ZOOM_MIN, ZOOM_MAX);
    const ratio = next / state.k;
    state.x = px - (px - state.x) * ratio;
    state.y = py - (py - state.y) * ratio;
    state.k = next;
    applyTransform(node, state);
  };

  node.addEventListener(
    "wheel",
    (event) => {
      // preventDefault needs a non-passive listener. ctrl+wheel is handled the
      // same as a plain wheel, which is what makes a trackpad pinch work.
      event.preventDefault();
      // Multiplicative, so Firefox reporting deltas in lines rather than
      // pixels changes the step size rather than the behavior.
      zoomAbout(event.clientX, event.clientY, Math.exp(-event.deltaY * 0.002));
    },
    { passive: false, signal },
  );

  let pointerId = -1;
  let panning = false;
  let startX = 0;
  let startY = 0;
  let originX = 0;
  let originY = 0;

  node.addEventListener(
    "pointerdown",
    (event) => {
      if (event.button !== 0) return;
      pointerId = event.pointerId;
      startX = event.clientX;
      startY = event.clientY;
      originX = state.x;
      originY = state.y;
      panning = false;
    },
    { signal },
  );

  node.addEventListener(
    "pointermove",
    (event) => {
      if (event.pointerId !== pointerId) return;
      const dx = event.clientX - startX;
      const dy = event.clientY - startY;
      if (!panning) {
        if (Math.hypot(dx, dy) < PAN_THRESHOLD_PX) return;
        panning = true;
        node.classList.add("is-panning");
        node.setPointerCapture(pointerId);
      }
      state.x = originX + dx;
      state.y = originY + dy;
      applyTransform(node, state);
    },
    { signal },
  );

  const endPan = (event: PointerEvent): void => {
    if (event.pointerId !== pointerId) return;
    if (panning && node.hasPointerCapture(pointerId)) {
      node.releasePointerCapture(pointerId);
    }
    node.classList.remove("is-panning");
    panning = false;
    pointerId = -1;
  };
  node.addEventListener("pointerup", endPan, { signal });
  node.addEventListener("pointercancel", endPan, { signal });

  node.addEventListener("dblclick", reset, { signal });

  node.addEventListener(
    "keydown",
    (event) => {
      const rect = node.getBoundingClientRect();
      const cx = rect.left + rect.width / 2;
      const cy = rect.top + rect.height / 2;
      if (event.key === "+" || event.key === "=") zoomAbout(cx, cy, 1.2);
      else if (event.key === "-") zoomAbout(cx, cy, 1 / 1.2);
      else if (event.key === "0") reset();
      else return;
      event.preventDefault();
    },
    { signal },
  );

  // On the document rather than the node: a live-reload refetch can remove the
  // fullscreen block outright, and the exit then fires on the document while
  // the node sits disconnected. This one edge covers the button, Esc, browser
  // chrome and element removal alike.
  document.addEventListener(
    "fullscreenchange",
    () => {
      if (document.fullscreenElement === node) return;
      reset();
      controller.abort();
    },
    { signal },
  );

  node.tabIndex = -1;
  node.focus();
}

// A diagram is often the widest thing on the page and the one thing a reader
// wants to fill the screen. The control is added after every run, because both
// the initial mount and the theme re-render replace the block's contents.
function addFullscreenControls(nodes: NodeListOf<Element>): void {
  nodes.forEach((el) => {
    const node = el as HTMLElement;
    if (!node.querySelector("svg")) return;
    if (node.querySelector(".mermaid-expand")) return;

    const button = document.createElement("button");
    button.type = "button";
    button.className = "mermaid-expand";
    button.title = "Expand diagram — scroll to zoom, drag to pan, 0 to reset";
    button.setAttribute("aria-label", "Expand diagram");
    button.addEventListener("click", () => {
      if (document.fullscreenElement === node) {
        void document.exitFullscreen();
        return;
      }
      // Not supported everywhere, and rejects when the gesture is not trusted;
      // the diagram stays inline in that case rather than throwing.
      void attempt(() => node.requestFullscreen()).then(([, err]) => {
        if (err) return;
        beginZoomSession(node);
      });
    });
    node.appendChild(button);
  });
}

// mountMermaid lazy-loads the carried mermaid vendor script (once per page
// load), stashes each diagram's raw source for a later re-render (theme
// toggle), and runs mermaid scoped to this container's <pre.mermaid> nodes
// only — a page swap must not re-render diagrams belonging to a page that's
// no longer mounted.
export async function mountMermaid(container: HTMLElement): Promise<void> {
  const nodes = container.querySelectorAll("pre.mermaid");
  if (nodes.length === 0) return;

  const [, loadErr] = await attempt(() => loadScript("/vendor/mermaid.min.js"));
  if (loadErr) {
    // Vendor script unavailable (offline, blocked, test environment with no
    // static server) — leave the raw <pre> visible rather than throwing out
    // of the mount effect.
    return;
  }
  if (!window.mermaid) return;

  nodes.forEach((el) => {
    const node = el as HTMLElement;
    if (!node.dataset.mermaidSrc) node.dataset.mermaidSrc = node.textContent ?? "";
  });

  window.mermaid.initialize({
    startOnLoad: false,
    securityLevel: "loose",
    theme: "base",
    themeVariables: mermaidThemeVars(),
  });
  // Malformed diagram source — leave the raw <pre> visible rather than
  // throwing out of the mount effect.
  await attempt(async () => window.mermaid?.run({ nodes }));
  addFullscreenControls(nodes);
}

// rethemeMermaid re-runs every already-mounted diagram in the document
// (data-mermaid-src stashed by mountMermaid) against the now-flipped CSS
// vars: restores each diagram's stashed source, drops mermaid's own
// data-processed flag, re-initializes with the new palette, and re-runs
// scoped to those nodes. A no-op with nothing mounted or the vendor script
// never loaded (page view with no diagrams, or theme toggled before any
// mermaid mount completed).
export async function rethemeMermaid(): Promise<void> {
  if (!window.mermaid) return;
  const nodes = document.querySelectorAll("pre.mermaid[data-mermaid-src]");
  if (nodes.length === 0) return;

  nodes.forEach((el) => {
    const node = el as HTMLElement;
    node.removeAttribute("data-processed");
    node.innerHTML = node.dataset.mermaidSrc ?? "";
  });

  window.mermaid.initialize({
    startOnLoad: false,
    securityLevel: "loose",
    theme: "base",
    themeVariables: mermaidThemeVars(),
  });
  await attempt(async () => window.mermaid?.run({ nodes }));
  addFullscreenControls(nodes);
}
