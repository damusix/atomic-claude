import { afterEach, beforeEach, describe, expect, test } from "bun:test";
import { __resetForTest, getDiagram, installDiagramClicks, openDiagram } from "./store";

const SVG_NS = "http://www.w3.org/2000/svg";

function renderedBlock(id = "mermaid-1"): { pre: HTMLElement; svg: SVGSVGElement } {
  const body = document.createElement("div");
  body.className = "md-content";
  const pre = document.createElement("pre");
  pre.className = "mermaid";
  pre.dataset.processed = "true";
  const svg = document.createElementNS(SVG_NS, "svg");
  svg.id = id;
  svg.setAttribute("viewBox", "0 0 300 120");
  const style = document.createElementNS(SVG_NS, "style");
  style.textContent = `#${id} .node rect { fill: red; }`;
  svg.appendChild(style);
  pre.appendChild(svg);
  body.appendChild(pre);
  document.body.appendChild(body);
  return { pre, svg };
}

describe("openDiagram", () => {
  afterEach(() => {
    __resetForTest();
    document.body.innerHTML = "";
  });

  // Mermaid scopes styles by svg id and cleans up by it on re-render; a copy
  // sharing the id would be restyled or removed along with the original.
  test("gives the copy its own id everywhere the original's appears", () => {
    const { svg } = renderedBlock("mermaid-7");
    openDiagram(svg);
    const markup = getDiagram()?.markup ?? "";
    expect(markup).toContain('id="mermaid-7-lightbox"');
    expect(markup).toContain("#mermaid-7-lightbox .node rect");
    expect(markup).not.toMatch(/mermaid-7(?!-lightbox)/);
  });

  test("records the drawing's natural size so the viewer can fit it to the window", () => {
    const { svg } = renderedBlock();
    openDiagram(svg);
    expect(getDiagram()).toMatchObject({ width: 300, height: 120 });
  });
});

describe("installDiagramClicks", () => {
  let uninstall: () => void = () => {};

  beforeEach(() => {
    uninstall = installDiagramClicks();
  });

  afterEach(() => {
    uninstall();
    __resetForTest();
    document.body.innerHTML = "";
  });

  test("clicking a rendered diagram opens it", () => {
    const { svg } = renderedBlock();
    svg.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(getDiagram()).not.toBeNull();
  });

  // Enter or Space on the focused button arrives as a click on it.
  test("the diagram's keyboard button opens it", () => {
    const { pre } = renderedBlock();
    const button = document.createElement("button");
    button.className = "mermaid-open";
    pre.appendChild(button);
    button.click();
    expect(getDiagram()).not.toBeNull();
  });

  test("a diagram still showing its raw source does not open", () => {
    const { pre, svg } = renderedBlock();
    pre.removeAttribute("data-processed");
    svg.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(getDiagram()).toBeNull();
  });

  // securityLevel "loose" lets a diagram carry links and click callbacks;
  // those must keep working instead of opening the viewer.
  test("links and click-bound nodes inside a diagram keep their own behavior", () => {
    const { svg } = renderedBlock();
    const link = document.createElementNS(SVG_NS, "a");
    const clickable = document.createElementNS(SVG_NS, "g");
    clickable.setAttribute("class", "node clickable");
    svg.append(link, clickable);

    link.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    clickable.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(getDiagram()).toBeNull();
  });
});
