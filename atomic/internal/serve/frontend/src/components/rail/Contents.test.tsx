import { afterEach, describe, expect, test } from "bun:test";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { emitPageHeadings } from "../../utils/events";
import { Contents } from "./Contents";

function renderContents(initialPath: string) {
  const router = createMemoryRouter([{ path: "*", element: <Contents /> }], { initialEntries: [initialPath] });
  return { router, ...render(<RouterProvider router={router} />) };
}

describe("Contents", () => {
  afterEach(() => {
    emitPageHeadings([]);
  });

  test("clicking a heading preserves ?at= and ?member= alongside the new hash (Plans route)", async () => {
    emitPageHeadings([{ id: "goal", text: "Goal", level: 1 }]);
    const { router } = renderContents("/plans/atomic-doctor/docs/spec/atomic-doctor.md?member=server&at=main");

    await userEvent.click(screen.getByText("Goal"));

    expect(router.state.location.search).toBe("?member=server&at=main");
    expect(router.state.location.hash).toBe("#goal");
  });

  test("clicking a heading on a /page/ route keeps working (search preserved, none to drop)", async () => {
    emitPageHeadings([{ id: "intro", text: "Intro", level: 1 }]);
    const { router } = renderContents("/page/wiki/index.md");

    await userEvent.click(screen.getByText("Intro"));

    expect(router.state.location.pathname).toBe("/page/wiki/index.md");
    expect(router.state.location.hash).toBe("#intro");
  });
});
