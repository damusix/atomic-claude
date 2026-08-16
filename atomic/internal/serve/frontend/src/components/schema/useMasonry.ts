// useMasonry — measures each card and tells the grid how many rows it spans.
//
// The grid's rows are a few pixels tall (--masonry-row), so a card spanning
// ceil(height / row) of them reserves almost exactly its own height. That is
// what lets auto-placement fill left to right without leaving a hole under
// every card shorter than its neighbour, which is what an ordinary grid row
// does. CSS cannot measure, so this is the one thing the layout needs JS for.
import { useLayoutEffect, type RefObject } from "react";

/** Fallback span for a card that has not been measured yet, in --masonry-row
    units. Roughly a mid-sized card, so the first paint is close rather than
    collapsed. */
const UNMEASURED_SPAN = 40;

function readPx(styles: CSSStyleDeclaration, prop: string, fallback: number): number {
  const value = parseFloat(styles.getPropertyValue(prop));
  return Number.isFinite(value) && value > 0 ? value : fallback;
}

/**
 * Keeps every direct child of `ref` spanning the right number of grid rows.
 *
 * Re-measures on its own whenever a card changes height, so expanding a
 * capped "+24 more" relation list reflows the wall instead of overlapping the
 * card below it. `deps` covers the cases where the children themselves are
 * replaced: a different member, a new filter, a fresh fetch.
 */
export function useMasonry(ref: RefObject<HTMLElement | null>, deps: unknown[]): void {
  useLayoutEffect(() => {
    const container = ref.current;
    if (!container) return;

    const styles = getComputedStyle(container);
    const row = readPx(styles, "--masonry-row", 8);
    const gap = readPx(styles, "--card-gap", 12);

    const measure = (item: HTMLElement) => {
      const height = item.getBoundingClientRect().height;
      if (!height) return;
      // The gap below a card lives inside its own span; the grid's row-gap is
      // zero, so a card that reserved only its own height would touch the one
      // beneath it.
      const span = Math.max(1, Math.ceil((height + gap) / row));
      item.style.setProperty("--span", String(span));
    };

    const items = Array.from(container.children).filter(
      (child): child is HTMLElement => child instanceof HTMLElement,
    );
    for (const item of items) measure(item);

    // Setting --span changes what the card reserves, never its own height, so
    // observing the cards we just measured cannot feed back into itself.
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        if (entry.target instanceof HTMLElement) measure(entry.target);
      }
    });
    for (const item of items) observer.observe(item);
    return () => observer.disconnect();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
}

export const __UNMEASURED_SPAN_FOR_TEST = UNMEASURED_SPAN;
