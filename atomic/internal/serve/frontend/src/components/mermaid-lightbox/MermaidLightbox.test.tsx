import { afterEach, describe, expect, test } from "bun:test";
import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MermaidLightbox } from "./MermaidLightbox";
import { __resetForTest, getDiagram, openDiagram } from "./store";

function diagramSvg(): SVGSVGElement {
  const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  svg.id = "mermaid-3";
  svg.setAttribute("viewBox", "0 0 200 100");
  return svg;
}

describe("MermaidLightbox", () => {
  afterEach(() => {
    act(() => __resetForTest());
  });

  test("shows an opened diagram in a dialog over the page", async () => {
    render(<MermaidLightbox />);
    act(() => openDiagram(diagramSvg()));
    const dialog = await screen.findByRole("dialog");
    expect(dialog.querySelector("svg#mermaid-3-lightbox")).not.toBeNull();
  });

  test("the close button closes it", async () => {
    render(<MermaidLightbox />);
    act(() => openDiagram(diagramSvg()));
    await userEvent.click(await screen.findByRole("button", { name: "Close diagram viewer" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(getDiagram()).toBeNull();
  });

  test("Escape closes it", async () => {
    render(<MermaidLightbox />);
    act(() => openDiagram(diagramSvg()));
    await screen.findByRole("dialog");
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
  });
});
