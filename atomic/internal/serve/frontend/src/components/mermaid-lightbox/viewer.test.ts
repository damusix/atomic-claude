import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { attachViewer, newViewState, type ViewState } from "./viewer";

const SIZE = { width: 400, height: 200 };

let viewport: HTMLElement;
let stage: HTMLElement;
let svg: SVGSVGElement;
let close: ReturnType<typeof mock>;
let view: ViewState;
let detach: () => void;

function pointer(type: string, target: Element, x: number, y: number) {
  target.dispatchEvent(new PointerEvent(type, { bubbles: true, pointerId: 1, clientX: x, clientY: y }));
}

beforeEach(() => {
  viewport = document.createElement("div");
  viewport.setPointerCapture = () => {};
  stage = document.createElement("div");
  svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  stage.appendChild(svg);
  viewport.appendChild(stage);
  document.body.appendChild(viewport);
  close = mock(() => {});
  view = newViewState();
  detach = attachViewer(viewport, stage, SIZE, close, view);
});

afterEach(() => {
  detach();
  document.body.innerHTML = "";
});

describe("attachViewer closing", () => {
  test("a click on empty space closes the viewer", () => {
    pointer("pointerdown", viewport, 10, 10);
    pointer("pointerup", viewport, 10, 10);
    viewport.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(close).toHaveBeenCalledTimes(1);
  });

  test("the click that ends a drag does not close it", () => {
    pointer("pointerdown", viewport, 10, 10);
    pointer("pointermove", viewport, 40, 30);
    pointer("pointerup", viewport, 40, 30);
    viewport.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(close).not.toHaveBeenCalled();
  });

  // Pointer capture retargets the click to the viewport, so where the press
  // started is what tells a click on the drawing apart from one beside it.
  test("a click on the drawing does not close it", () => {
    pointer("pointerdown", svg, 100, 100);
    pointer("pointerup", viewport, 100, 100);
    viewport.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(close).not.toHaveBeenCalled();
  });
});

describe("attachViewer zoom", () => {
  test("+ zooms in and 0 fits the diagram to the window again", () => {
    const fitted = view.scale;
    viewport.dispatchEvent(new KeyboardEvent("keydown", { key: "+" }));
    expect(view.scale).toBeGreaterThan(fitted);
    viewport.dispatchEvent(new KeyboardEvent("keydown", { key: "0" }));
    expect(view.scale).toBe(fitted);
  });

  // The dialog re-attaches whenever its subtree re-renders; a reader zoomed in
  // must stay zoomed in.
  test("re-attaching keeps the current zoom and position instead of refitting", () => {
    viewport.dispatchEvent(new KeyboardEvent("keydown", { key: "+" }));
    const zoomed = stage.style.transform;
    detach();
    stage.style.transform = "";
    detach = attachViewer(viewport, stage, SIZE, close, view);
    expect(stage.style.transform).toBe(zoomed);
  });
});
