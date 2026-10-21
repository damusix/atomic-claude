import { describe, expect, test } from "bun:test";
import { usesElk, withElk } from "./mermaidLayout";

describe("usesElk", () => {
  test("lays out flowchart, state, class, ER and requirement diagrams with ELK", () => {
    const types = ["flowchart-v2", "flowchart", "flowchart-elk", "stateDiagram", "state", "classDiagram", "class", "er", "requirement"];
    for (const type of types) expect(usesElk(type)).toBe(true);
  });

  // Mindmap throws under ELK and loses its radial layout under dagre, so it,
  // and every type not known to gain from ELK, gets no layout key at all.
  test("leaves mindmap, sequence and unrecognized diagrams to their own layout", () => {
    expect(usesElk("mindmap")).toBe(false);
    expect(usesElk("sequence")).toBe(false);
    expect(usesElk(null)).toBe(false);
  });
});

describe("withElk", () => {
  test("puts the layout directive ahead of a plain diagram", () => {
    expect(withElk("flowchart TD\n  A-->B")).toBe('%%{init: {"layout": "elk"}}%%\nflowchart TD\n  A-->B');
  });

  // Ahead of the frontmatter, mermaid fails to parse it.
  test("puts the directive after a frontmatter block, not before it", () => {
    const src = "---\ntitle: Plan\n---\nflowchart TD\n  A-->B";
    expect(withElk(src)).toBe('---\ntitle: Plan\n---\n%%{init: {"layout": "elk"}}%%\nflowchart TD\n  A-->B');
  });

  // The directive would override the author's frontmatter choice.
  test("leaves a diagram alone when its frontmatter already picks a layout", () => {
    const src = "---\nconfig:\n  layout: dagre\n---\nflowchart TD\n  A-->B";
    expect(withElk(src)).toBe(src);
  });
});
