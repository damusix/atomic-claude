import { describe, expect, mock, test } from "bun:test";
import { render } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { useHashScroll } from "./useHashScroll";

function HashScrollProbe() {
  useHashScroll();
  return (
    <div>
      <div id="anchor">target</div>
    </div>
  );
}

describe("useHashScroll", () => {
  test("scrolls the hash target into view on mount", () => {
    const scrollIntoView = mock(() => {});
    HTMLElement.prototype.scrollIntoView = scrollIntoView;

    render(
      <MemoryRouter initialEntries={["/page/wiki/index.md#anchor"]}>
        <HashScrollProbe />
      </MemoryRouter>,
    );

    expect(scrollIntoView).toHaveBeenCalledTimes(1);
  });

  test("does nothing when there is no hash", () => {
    const scrollIntoView = mock(() => {});
    HTMLElement.prototype.scrollIntoView = scrollIntoView;

    render(
      <MemoryRouter initialEntries={["/page/wiki/index.md"]}>
        <HashScrollProbe />
      </MemoryRouter>,
    );

    expect(scrollIntoView).not.toHaveBeenCalled();
  });
});
