// typeColors — single-source OKF type -> color derivation. Ports
// templates/layout.html's atomicCyTypeColors()/atomicRampColors()/darken()
// verbatim (same CSS-var reads, same fallbacks, same field names) so every
// consumer — the carried graph-core.js/system-graph.js/code-graph.js
// profiles (vanilla <script> globals) and React (railCytoscapeStyle, CP6) —
// derives colors from exactly one place. Do not re-derive colors elsewhere.
//
// Exposed on `window` under the same bare names the carried scripts already
// call unqualified (`atomicCyTypeColors()`, `atomicRampColors()`) — those
// files are plain, non-module <script>s, so an unqualified call resolves
// through the global scope to whatever `window.<name>` holds.

export type OkfType =
  | "page"
  | "repo"
  | "domain"
  | "concern"
  | "knowledge"
  | "bucket"
  | "index"
  | "external";

type RampShades = { bright: string[]; dusky: string[] };

// The dark theme's bright+dusky ramps (docs/spec/code-graph.md's 8-hue x
// 5-shade x 2-band palette), used only when a --ramp-* CSS var comes back
// empty (stylesheet failed to load, etc.).
const RAMP_FALLBACK: Record<string, RampShades> = {
  gold: {
    bright: ["#f7e1a8", "#f5c860", "#e0a83a", "#c08a26", "#96691a"],
    dusky: ["#d6c08d", "#c2a45f", "#a58743", "#866b30", "#665022"],
  },
  slate: {
    bright: ["#c3d0e8", "#9fb4d6", "#7d94bd", "#5f769e", "#47597b"],
    dusky: ["#a4afc5", "#8c98ac", "#6d7b95", "#566174", "#3f4757"],
  },
  moss: {
    bright: ["#d5d9a8", "#b8c078", "#99a355", "#7b843d", "#5e662c"],
    dusky: ["#adaf8f", "#898d6e", "#6b6e52", "#555a34", "#3e4224"],
  },
  plum: {
    bright: ["#dcc4ea", "#c19fd9", "#a57dc0", "#875f9f", "#68477c"],
    dusky: ["#bba3c8", "#a18bb0", "#876c99", "#695674", "#4f3f58"],
  },
  magenta: {
    bright: ["#eab6ca", "#d98ba9", "#bf6a8b", "#9e506e", "#7a3a53"],
    dusky: ["#c994a9", "#b0778d", "#955c72", "#724858", "#553340"],
  },
  terra: {
    bright: ["#f2c4a0", "#e89f68", "#d17f42", "#ad6330", "#854a22"],
    dusky: ["#d7a279", "#c38150", "#9e663d", "#7c4e2d", "#5c381f"],
  },
  cyan: {
    bright: ["#b3dfe0", "#85c5c8", "#62a8ab", "#48878a", "#346467"],
    dusky: ["#97b9b9", "#789798", "#5c7879", "#3e5f60", "#2b4244"],
  },
  gray: {
    bright: ["#d9d2c5", "#bab2a2", "#9a917f", "#7b7263", "#5d564a"],
    dusky: ["#b5aea0", "#968f80", "#787163", "#5c564b", "#423d35"],
  },
};

// Maps each OKF type to its ramp hue — docs graph reads the DUSKY band.
const TYPE_HUE: Record<OkfType, string> = {
  page: "gold",
  repo: "slate",
  domain: "moss",
  concern: "plum",
  knowledge: "magenta",
  bucket: "terra",
  index: "cyan",
  external: "gray",
};

// Accepts a 6-digit hex string ("#rrggbb") and returns a new hex string
// darkened by `amount` (0..1).
function darken(hex: string, amount: number): string {
  const h = hex.replace("#", "");
  if (h.length !== 6) return hex;
  const f = 1 - amount;
  const channel = (start: number) => {
    const c = Math.round(parseInt(h.slice(start, start + 2), 16) * f);
    return Math.max(0, Math.min(255, c))
      .toString(16)
      .padStart(2, "0");
  };
  return "#" + channel(0) + channel(2) + channel(4);
}

function readVar(style: CSSStyleDeclaration, name: string, fallback = ""): string {
  return style.getPropertyValue(name).trim() || fallback;
}

// atomicRampColors reads the 8-hue x 5-shade x 2-band ramp vars (app.css's
// --ramp-<hue>-<n> / --ramp-<hue>-dusky-<n>) into one flat map keyed
// "<hue>-<n>" (bright) / "<hue>-dusky-<n>" (dusky), n=1..5.
export function atomicRampColors(): Record<string, string> {
  const style = getComputedStyle(document.documentElement);
  const out: Record<string, string> = {};
  for (const hue of Object.keys(RAMP_FALLBACK)) {
    const shades = RAMP_FALLBACK[hue];
    for (let n = 1; n <= 5; n++) {
      out[`${hue}-${n}`] = readVar(style, `--ramp-${hue}-${n}`, shades.bright[n - 1]);
      out[`${hue}-dusky-${n}`] = readVar(style, `--ramp-${hue}-dusky-${n}`, shades.dusky[n - 1]);
    }
  }
  return out;
}

// Every entry is either a hex color string or (for the "<type>-ramp" keys) the
// 5-shade dusky ramp array a caller indexes by degree quintile.
export type TypeColorMap = Record<string, string | string[]>;

// atomicCyTypeColors reads ALL graph color tokens from CSS custom properties
// so the graph re-themes automatically on light/dark toggle. Provides safe
// fallbacks for every entry.
export function atomicCyTypeColors(): TypeColorMap {
  const style = getComputedStyle(document.documentElement);
  const ramps = atomicRampColors();
  const out: TypeColorMap = {
    "default-label": readVar(style, "--ink", "#211d18"),
    // Label chip: a paper-surface pill behind each label so text stays
    // legible over the graph canvas in both themes.
    "label-bg": readVar(style, "--paper-raised", "#ffffff"),
    "label-border": readVar(style, "--hairline-2", "#e2ddd2"),
    edge: readVar(style, "--edge", "#cabfae"),
    "edge-strong": readVar(style, "--edge-strong", "#b1a48f"),
    selected: readVar(style, "--amber-bright", "#d99a1f"),
  };

  for (const type of Object.keys(TYPE_HUE) as OkfType[]) {
    const hue = TYPE_HUE[type];
    // Full 5-shade dusky ramp for this type — degree-quintile shading
    // indexes into this (quintile 1..5 maps to index 0..4).
    const duskyRamp = [1, 2, 3, 4, 5].map((n) => ramps[`${hue}-dusky-${n}`]);
    const fill = readVar(style, `--c-${type}`) || duskyRamp[1];
    const ink = readVar(style, `--c-${type}-ink`) || duskyRamp[0];
    out[type] = fill;
    out[`${type}-ink`] = ink;
    // Gradient dark stop: the next-deeper ramp shade (3) when resolvable,
    // else the old 10%-darken transform as a last-resort fallback.
    out[`${type}-dark`] = duskyRamp[2] || darken(fill, 0.1);
    out[`${type}-ramp`] = duskyRamp;
  }

  // Default node colors (unknown/empty type) mirror the 'page' type.
  out["default-fill"] = out.page;
  out["default-dark"] = out["page-dark"];
  out["default-ramp"] = out["page-ramp"];

  return out;
}

declare global {
  interface Window {
    atomicCyTypeColors: typeof atomicCyTypeColors;
    atomicRampColors: typeof atomicRampColors;
  }
}

// Exposed for the carried vanilla <script>s (graph-core.js, system-graph.js,
// code-graph.js) — see the module comment above.
export function installTypeColorsGlobal(target: Window = window): void {
  target.atomicCyTypeColors = atomicCyTypeColors;
  target.atomicRampColors = atomicRampColors;
}
