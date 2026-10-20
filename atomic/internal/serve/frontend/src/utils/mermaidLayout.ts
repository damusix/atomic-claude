// utils/mermaidLayout — which diagrams ELK lays out. Shared with the docs site
// (.vitepress/theme/mermaid-render.ts), which imports this file.

// detectType ids ELK handles. No other type gets a layout key: mindmap throws
// under ELK and turns into a layered tree under dagre.
const ELK_TYPES = new Set([
  "flowchart-v2",
  "flowchart",
  "flowchart-elk",
  "stateDiagram",
  "state",
  "classDiagram",
  "class",
  "er",
  "requirement",
]);

const ELK = '%%{init: {"layout": "elk"}}%%';
const FRONTMATTER = /^\s*---\r?\n[\s\S]*?\r?\n---[^\S\r\n]*(?:\r?\n|$)/;
const LAYOUT_KEY = /^\s+layout\s*:/m;

export function usesElk(type: string | null): boolean {
  return type !== null && ELK_TYPES.has(type);
}

// Ahead of a frontmatter block the directive breaks its parse, so it follows one;
// a directive overrides frontmatter, so an author's own `layout` is left alone.
export function withElk(src: string): string {
  const frontmatter = FRONTMATTER.exec(src)?.[0] ?? "";
  if (LAYOUT_KEY.test(frontmatter)) return src;
  return `${frontmatter}${ELK}\n${src.slice(frontmatter.length)}`;
}
