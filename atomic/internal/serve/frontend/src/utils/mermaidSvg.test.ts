import { describe, expect, test } from "bun:test";
import { chipLabels } from "./mermaidSvg";

const SVG_NS = "http://www.w3.org/2000/svg";

function el<K extends keyof SVGElementTagNameMap>(tag: K, cls?: string): SVGElementTagNameMap[K] {
  const node = document.createElementNS(SVG_NS, tag);
  if (cls) node.setAttribute("class", cls);
  return node;
}

function sized(node: SVGGraphicsElement, box: { x: number; y: number; width: number; height: number }) {
  node.getBBox = () => DOMRect.fromRect(box);
}

function placed(node: SVGGraphicsElement) {
  node.getScreenCTM = () => new DOMMatrix();
}

describe("chipLabels", () => {
  test("puts a sequence message on a canvas-colored chip padded 3px", () => {
    const svg = el("svg");
    const text = el("text", "messageText");
    text.textContent = "commit";
    sized(text, { x: 100, y: 40, width: 50, height: 14 });
    svg.appendChild(text);

    chipLabels(svg, "#fff6e5");

    const chip = svg.querySelector("rect.label-chip");
    expect(chip?.getAttribute("fill")).toBe("#fff6e5");
    expect([chip?.getAttribute("x"), chip?.getAttribute("y")]).toEqual(["97", "37"]);
    expect([chip?.getAttribute("width"), chip?.getAttribute("height")]).toEqual(["56", "20"]);
    expect(chip?.nextElementSibling).toBe(text);
  });

  // Not fully opaque, so a line under a label still reads as passing beneath it.
  test("leaves the chip slightly see-through", () => {
    const svg = el("svg");
    const text = el("text", "messageText");
    sized(text, { x: 0, y: 0, width: 40, height: 12 });
    svg.appendChild(text);

    chipLabels(svg, "#fff6e5");

    expect(svg.querySelector<SVGRectElement>("rect.label-chip")?.style.opacity).toBe("0.9");
  });

  // Mermaid paints activation bars after the message text, so a label left in
  // place disappears under the bar wherever they cross.
  test("lifts the label above shapes mermaid drew after it", () => {
    const svg = el("svg");
    const text = el("text", "messageText");
    const activation = el("rect", "activation0");
    sized(text, { x: 0, y: 0, width: 40, height: 12 });
    placed(svg);
    placed(text);
    svg.append(text, activation);

    chipLabels(svg, "#141210");

    expect(svg.lastElementChild?.getAttribute("class")).toBe("label-chips");
    expect(svg.lastElementChild?.contains(text)).toBe(true);
  });

  test("an edge label keeps its own transform and takes the chip as its first child", () => {
    const svg = el("svg");
    const label = el("g", "edgeLabel");
    label.setAttribute("transform", "translate(80, 120)");
    label.appendChild(el("foreignObject"));
    sized(label, { x: -20, y: -9, width: 40, height: 18 });
    svg.appendChild(label);

    chipLabels(svg, "#fff6e5");

    expect(label.firstElementChild?.getAttribute("class")).toBe("label-chip");
    expect(label.getAttribute("transform")).toBe("translate(80, 120)");
  });

  test("an empty label, like an unlabeled edge, gets no chip", () => {
    const svg = el("svg");
    const label = el("g", "edgeLabel");
    sized(label, { x: 0, y: 0, width: 0, height: 0 });
    svg.appendChild(label);

    chipLabels(svg, "#fff6e5");

    expect(svg.querySelector("rect.label-chip")).toBeNull();
  });
});
