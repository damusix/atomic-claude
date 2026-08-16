import { afterEach, describe, expect, mock, test } from "bun:test";
import { atomicCyTypeColors, graphTypeColors } from "../../utils/typeColors";
import { railCytoscapeStyle, registerRailCy, rethemeRailGraph } from "./railCytoscapeStyle";

describe("railCytoscapeStyle", () => {
  test("is built from typeColors — the default node fill matches graphTypeColors()'s default-fill/default-dark", () => {
    // graphTypeColors, not atomicCyTypeColors: the rail paints from the same
    // vivid band as the full-page graph so a type is one colour app-wide.
    const colors = graphTypeColors();
    const rules = railCytoscapeStyle("some/page.md");

    const defaultRule = rules.find((r) => r.selector === "node");
    expect(defaultRule).toBeDefined();
    expect(defaultRule?.style["background-gradient-stop-colors"]).toBe(
      `${colors["default-fill"]} ${colors["default-dark"]}`,
    );
  });

  test("emits one per-type node selector for every OKF type, colored and shaped from typeColors", () => {
    const colors = graphTypeColors();
    const rules = railCytoscapeStyle("focus");

    for (const type of ["page", "repo", "concern", "knowledge", "bucket", "external", "index", "domain"]) {
      const rule = rules.find((r) => r.selector === `node[type="${type}"]`);
      expect(rule).toBeDefined();
      const expectedFill = colors[type];
      expect(rule?.style["background-gradient-stop-colors"]).toContain(String(expectedFill));
      // Shape is the second channel — a type that only differed by hue was
      // unreadable at the rail's 8px dots.
      expect(rule?.style.shape).toBeDefined();
    }
  });

  test("highlights the focus node with the selected/amber gradient", () => {
    const colors = atomicCyTypeColors();
    const rules = railCytoscapeStyle("wiki/index.md");

    const focusRule = rules.find((r) => r.selector === 'node[id="wiki/index.md"]');
    expect(focusRule).toBeDefined();
    expect(focusRule?.style["background-gradient-stop-colors"]).toContain(String(colors.selected));
  });

  test("the compact rail node overrides width/height down to 10px (not the full-graph degree-based sizing)", () => {
    const rules = railCytoscapeStyle("x");
    const defaultRule = rules.find((r) => r.selector === "node");
    expect(defaultRule?.style.width).toBe("10px");
    expect(defaultRule?.style.height).toBe("10px");
  });

  test("carries the md-link and wikilink edge selectors", () => {
    const rules = railCytoscapeStyle("x");
    expect(rules.some((r) => r.selector === "edge.md-link")).toBe(true);
    expect(rules.some((r) => r.selector === "edge.wikilink")).toBe(true);
  });
});

describe("registerRailCy / rethemeRailGraph", () => {
  afterEach(() => {
    delete window.__railCy;
  });

  test("no-ops with nothing registered", () => {
    expect(() => rethemeRailGraph()).not.toThrow();
  });

  test("re-applies railCytoscapeStyle for the registered focus node to the registered instance", () => {
    const style = mock(() => {});
    registerRailCy({ style }, "wiki/index.md");

    rethemeRailGraph();

    expect(style).toHaveBeenCalledTimes(1);
    const call = style.mock.calls[0] as unknown as [ReturnType<typeof railCytoscapeStyle>];
    expect(call[0]).toEqual(railCytoscapeStyle("wiki/index.md"));
  });
});
