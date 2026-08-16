import { afterEach, describe, expect, test } from "bun:test";
import { render } from "@testing-library/react";
import { useRef } from "react";
import { useMasonry } from "./useMasonry";

// happy-dom lays nothing out, so heights come from a data attribute. The
// custom properties are left unset on purpose: the hook falls back to the same
// 8px row and 12px gap the stylesheet declares, so the arithmetic under test is
// the arithmetic that ships.
const ROW = 8;
const GAP = 12;

const original = Element.prototype.getBoundingClientRect;

function stubHeights() {
  Element.prototype.getBoundingClientRect = function (this: Element) {
    const height = Number((this as HTMLElement).dataset?.h ?? 0);
    return { height, width: 288, top: 0, left: 0, right: 288, bottom: height, x: 0, y: 0, toJSON: () => ({}) } as DOMRect;
  };
}

function Harness({ heights }: { heights: number[] }) {
  const ref = useRef<HTMLDivElement>(null);
  useMasonry(ref, [heights]);
  return (
    <div ref={ref} data-testid="grid">
      {heights.map((h, i) => (
        <div key={i} data-testid={`card-${i}`} data-h={h ? String(h) : undefined} />
      ))}
    </div>
  );
}

function spans(container: HTMLElement): (string | null)[] {
  return [...container.children].map((c) => (c as HTMLElement).style.getPropertyValue("--span") || null);
}

describe("useMasonry", () => {
  afterEach(() => {
    Element.prototype.getBoundingClientRect = original;
  });

  // The whole contract: a card reserves its own height plus the gap beneath
  // it, rounded up to whole rows. Reserve less and the card below overlaps it;
  // reserve the row's height instead and every short card leaves a hole, which
  // is the grid behaviour this exists to avoid.
  test("a card spans its own height plus the gap, rounded up", () => {
    stubHeights();
    const { getByTestId } = render(<Harness heights={[100, 148, 667]} />);
    expect(spans(getByTestId("grid"))).toEqual([
      String(Math.ceil((100 + GAP) / ROW)),
      String(Math.ceil((148 + GAP) / ROW)),
      String(Math.ceil((667 + GAP) / ROW)),
    ]);
  });

  test("rounds up rather than down, so a card is never under-reserved", () => {
    stubHeights();
    // 101 + 12 = 113, which is 14.125 rows.
    const { getByTestId } = render(<Harness heights={[101]} />);
    expect(spans(getByTestId("grid"))).toEqual(["15"]);
  });

  // A card that measures zero has not been laid out yet. Writing a span of 1
  // for it would collapse it to 8px and let the next card sit on top.
  test("leaves an unmeasured card alone rather than collapsing it", () => {
    stubHeights();
    const { getByTestId } = render(<Harness heights={[0]} />);
    expect(spans(getByTestId("grid"))).toEqual([null]);
  });

  test("an empty container is not an error", () => {
    stubHeights();
    const { getByTestId } = render(<Harness heights={[]} />);
    expect(spans(getByTestId("grid"))).toEqual([]);
  });
});
