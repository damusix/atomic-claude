import { describe, expect, test } from "bun:test";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { COLLAPSED_LIMIT, EdgeList } from "./EdgeList";
import { dedupeEdges } from "./edges";
import type { RailEdge } from "./types";

function pageEdge(name: string): RailEdge {
  return {
    target: `docs/${name}.md`,
    resolvedPath: `docs/${name}.md`,
    broken: false,
    dir: false,
    ambiguous: false,
    codeFile: false,
    external: false,
  };
}

function renderList(count: number) {
  const edges = Array.from({ length: count }, (_, i) => pageEdge(`page-${i}`));
  return render(
    <MemoryRouter>
      <EdgeList views={dedupeEdges(edges)} />
    </MemoryRouter>,
  );
}

const rows = () => document.querySelectorAll(".rail-edge-list li").length;

describe("EdgeList", () => {
  test("shows every row when the list is under the cap, with no toggle", () => {
    renderList(5);

    expect(rows()).toBe(5);
    expect(screen.queryByRole("button")).toBeNull();
  });

  // A truncated list with no way to reach the rest is missing data, not a
  // tidy summary — the count itself has to be the way through.
  test("caps a long list and expands to the full set on click", async () => {
    const total = COLLAPSED_LIMIT + 14;
    renderList(total);

    expect(rows()).toBe(COLLAPSED_LIMIT);
    const toggle = screen.getByRole("button", { name: `showing ${COLLAPSED_LIMIT} of ${total} +` });

    await userEvent.click(toggle);
    expect(rows()).toBe(total);

    // And back: expanding is reversible, so a long list cannot permanently
    // take over the rail's scroll.
    await userEvent.click(screen.getByRole("button", { name: `showing all ${total} −` }));
    expect(rows()).toBe(COLLAPSED_LIMIT);
  });

  test("every row carries a kind glyph, not only directories", () => {
    renderList(3);

    expect(document.querySelectorAll(".rail-edge-list li .rail-edge-glyph").length).toBe(3);
  });
});
