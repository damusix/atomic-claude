// utils/mermaid — mounts fenced ```mermaid blocks (server-emitted as
// <pre class="mermaid">…raw…</pre>, see render.go's mermaidCodeRenderer)
// within a container. Ports templates/layout.html's atomicMermaidInit()/
// atomicMermaidThemeVars() minimally for CP6 (initial mount only — the
// light/dark retheme re-run belongs to CP11's useTheme retheme cascade).
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

function mermaidThemeVars(): Record<string, string> {
  const s = getComputedStyle(document.documentElement);
  const ink = readVar(s, "--ink", "#211d18");
  const surface = readVar(s, "--paper-raised", "#ffffff");
  const sunk = readVar(s, "--paper-sunk", "#f5f3ee");
  const paper = readVar(s, "--paper", "#fbfaf8");
  const border = readVar(s, "--hairline-2", "#e2ddd2");
  const line = readVar(s, "--edge-strong", "#b1a48f");
  const sans = readVar(s, "--sans", '"Inter", system-ui, sans-serif');
  return {
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
  };
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
}
