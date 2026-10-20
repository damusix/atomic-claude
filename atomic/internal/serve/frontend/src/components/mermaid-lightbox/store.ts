// store — the diagram the lightbox shows, read via useSyncExternalStore.
// Opened by installDiagramClicks below, closed by the dialog.

export interface DiagramView {
  markup: string;
  width: number;
  height: number;
}

type Listener = () => void;

let current: DiagramView | null = null;
const listeners = new Set<Listener>();

function set(next: DiagramView | null): void {
  current = next;
  for (const listener of listeners) listener();
}

export function subscribe(listener: Listener): () => void {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

export function getDiagram(): DiagramView | null {
  return current;
}

function naturalSize(svg: SVGSVGElement): { width: number; height: number } {
  const viewBox = svg.viewBox?.baseVal;
  if (viewBox && viewBox.width > 0 && viewBox.height > 0) {
    return { width: viewBox.width, height: viewBox.height };
  }
  const rect = svg.getBoundingClientRect();
  return { width: Math.max(rect.width, 1), height: Math.max(rect.height, 1) };
}

export function openDiagram(svg: SVGSVGElement): void {
  // Mermaid scopes styles and marker url(#…) refs by svg id and cleans up by it
  // on re-render, so a retheme would reach into a copy that kept the id.
  const markup = svg.id ? svg.outerHTML.split(svg.id).join(`${svg.id}-lightbox`) : svg.outerHTML;
  set({ markup, ...naturalSize(svg) });
}

export function closeDiagram(): void {
  set(null);
}

// Links and click-bound nodes inside a diagram keep their own behavior;
// securityLevel "loose" lets a diagram carry both.
export function installDiagramClicks(): () => void {
  const onClick = (event: MouseEvent) => {
    if (!(event.target instanceof Element)) return;
    const block = event.target.closest('.md-content pre.mermaid[data-processed="true"]');
    if (!block || event.target.closest("a, .clickable")) return;
    const svg = block.querySelector("svg");
    if (svg) openDiagram(svg);
  };
  document.addEventListener("click", onClick);
  return () => document.removeEventListener("click", onClick);
}

export function __resetForTest(): void {
  set(null);
}
