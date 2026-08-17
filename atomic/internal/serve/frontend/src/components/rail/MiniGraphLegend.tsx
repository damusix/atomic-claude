// MiniGraphLegend — what the mini-graph's colours and shapes mean.
//
// Built from the same CY_SHAPE / graphTypeColors pair the stylesheet uses, so
// the legend cannot drift from the marks it describes. Only the types actually
// present are listed: a fixed eight-row key would mostly describe things that
// are not on screen.
import { CY_SHAPE, DIR_COLOR, DIR_LABEL } from "./railCytoscapeStyle";
import { graphTypeColors } from "../../utils/typeColors";

// Cytoscape shape name -> a CSS clip-path drawing the same outline. Mirrors
// the graph legend's swatch rules in app.css; kept here because these swatches
// are inline-styled from the live palette rather than class-driven.
const CLIP: Record<string, string> = {
  hexagon: "polygon(25% 0%, 75% 0%, 100% 50%, 75% 100%, 25% 100%, 0% 50%)",
  rectangle: "none",
  triangle: "polygon(50% 0%, 100% 100%, 0% 100%)",
  star: "polygon(50% 0%, 61% 35%, 98% 35%, 68% 57%, 79% 91%, 50% 70%, 21% 91%, 32% 57%, 2% 35%, 39% 35%)",
  pentagon: "polygon(50% 0%, 100% 38%, 82% 100%, 18% 100%, 0% 38%)",
  diamond: "polygon(50% 0%, 100% 50%, 50% 100%, 0% 50%)",
  tag: "polygon(0% 0%, 68% 0%, 100% 50%, 68% 100%, 0% 100%)",
};

export function MiniGraphLegend({ types }: { types: string[] }) {
  if (!types.length) return null;
  const colors = graphTypeColors();

  return (
    <>
      {/* Direction first: it is the encoding specific to this panel, and the
          one a reader has no chance of guessing. */}
      <ul className="mini-legend" aria-label="Link direction key">
        {(["out", "in", "both"] as const).map((dir) => (
          <li className="mini-legend-item" key={dir}>
            <span className="mini-legend-line" style={{ background: DIR_COLOR[dir] }} />
            {DIR_LABEL[dir]}
          </li>
        ))}
      </ul>
      <ul className="mini-legend" aria-label="Node type key">
        {types.map((t) => {
        const shape = CY_SHAPE[t] ?? "ellipse";
        const fill = colors[t];
        return (
          <li className="mini-legend-item" key={t}>
            <span
              className="mini-legend-swatch"
              style={{
                background: typeof fill === "string" ? fill : undefined,
                borderRadius: shape === "ellipse" ? "50%" : undefined,
                clipPath: CLIP[shape] === "none" ? undefined : CLIP[shape],
              }}
            />
              {t}
            </li>
          );
        })}
      </ul>
    </>
  );
}
