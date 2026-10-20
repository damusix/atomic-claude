// utils/mermaidSvg — adjustments to a rendered diagram's svg. Shared with the docs
// site (.vitepress/theme/mermaid-render.ts), which imports this file.
const SVG_NS = "http://www.w3.org/2000/svg";
const CHIP_PAD = 3;

// The canvas padding both stylesheets put on a diagram's svg (app.css, custom.css).
export const CANVAS_PADDING = 12;

// Sequence message text and block conditions (sectionTitle is an else branch's,
// or a gantt section's), and flowchart/state edge labels.
const LABELS = "text.messageText, text.loopText, text.sectionTitle, g.edgeLabel";

function chipFor(box: DOMRect, fill: string): SVGRectElement {
  const chip = document.createElementNS(SVG_NS, "rect");
  chip.setAttribute("class", "label-chip");
  chip.setAttribute("x", String(box.x - CHIP_PAD));
  chip.setAttribute("y", String(box.y - CHIP_PAD));
  chip.setAttribute("width", String(box.width + CHIP_PAD * 2));
  chip.setAttribute("height", String(box.height + CHIP_PAD * 2));
  chip.setAttribute("rx", String(CHIP_PAD));
  chip.setAttribute("fill", fill);
  // See-through enough that a line under the label still shows as passing under it.
  // Inline: mermaid's id-scoped `.edgeLabel rect { opacity: 0.5 }` beats class rules.
  chip.style.opacity = "0.9";
  return chip;
}

// Pairs a <text> with its chip; a <g> label takes the chip as its first child.
function withChip(label: SVGGraphicsElement, chip: SVGRectElement): SVGGraphicsElement {
  if (!(label instanceof SVGTextElement)) {
    label.prepend(chip);
    return label;
  }
  const transform = label.getAttribute("transform");
  if (transform) chip.setAttribute("transform", transform);
  const pair = document.createElementNS(SVG_NS, "g");
  label.replaceWith(pair);
  pair.append(chip, label);
  return pair;
}

// Sets each label on a canvas-colored chip and lifts it above the drawing, so no
// line, activation bar or box paints over a word.
export function chipLabels(svg: SVGSVGElement, fill: string): void {
  const svgCTM = svg.getScreenCTM();
  const layer = document.createElementNS(SVG_NS, "g");
  layer.setAttribute("class", "label-chips");

  for (const label of svg.querySelectorAll<SVGGraphicsElement>(LABELS)) {
    const box = label.getBBox();
    if (box.width === 0 || box.height === 0) continue;
    const parent = label.parentElement;
    const parentCTM = parent instanceof SVGGraphicsElement ? parent.getScreenCTM() : null;
    const chipped = withChip(label, chipFor(box, fill));
    if (!svgCTM || !parentCTM) continue;

    // Re-parented under the svg, so it keeps its place by carrying over the
    // transform of the container it left.
    const m = svgCTM.inverse().multiply(parentCTM);
    const wrapper = document.createElementNS(SVG_NS, "g");
    wrapper.setAttribute("transform", `matrix(${m.a} ${m.b} ${m.c} ${m.d} ${m.e} ${m.f})`);
    wrapper.append(chipped);
    layer.append(wrapper);
  }

  if (layer.childElementCount > 0) svg.append(layer);
}

// Mermaid caps the svg at its natural width inline; under border-box sizing the
// canvas padding comes out of that cap and shrinks the drawing unless it grows.
export function makeRoomForPadding(svg: SVGSVGElement): void {
  const cap = Number.parseFloat(svg.style.maxWidth);
  if (Number.isFinite(cap)) svg.style.maxWidth = `${cap + CANVAS_PADDING * 2}px`;
}
