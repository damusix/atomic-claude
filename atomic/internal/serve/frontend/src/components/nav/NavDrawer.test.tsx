import { afterEach, describe, expect, mock, test } from "bun:test";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { ApiProvider } from "../../utils/api";
import { NavDrawer } from "./NavDrawer";

// NavTree fetches /api/nav on mount; flushing it inside act() keeps the
// resolution from landing as an unwrapped update after the test body.
async function flush() {
  await act(async () => {});
}

function renderDrawer(open: boolean, onClose = () => {}) {
  return render(
    <MemoryRouter>
      <ApiProvider>
        <NavDrawer open={open} onClose={onClose} />
      </ApiProvider>
    </MemoryRouter>,
  );
}

describe("NavDrawer", () => {
  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  const originalFetch = globalThis.fetch;

  test("Escape closes an open drawer", async () => {
    globalThis.fetch = mock(
      async () =>
        new Response(JSON.stringify({ scope: "repo", groups: [] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ) as unknown as typeof fetch;

    const onClose = mock(() => {});
    renderDrawer(true, onClose);
    await flush();

    await userEvent.keyboard("{Escape}");
    expect(onClose).toHaveBeenCalledTimes(1);
  });

  test("Escape is inert while the drawer is already closed", async () => {
    globalThis.fetch = mock(
      async () =>
        new Response(JSON.stringify({ scope: "repo", groups: [] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ) as unknown as typeof fetch;

    const onClose = mock(() => {});
    renderDrawer(false, onClose);
    await flush();

    await userEvent.keyboard("{Escape}");
    expect(onClose).not.toHaveBeenCalled();
  });

  // The tree stays mounted when closed so it keeps its fetched data and the
  // folders the reader expanded — remounting would refetch and collapse both.
  test("keeps the tree mounted (but hidden) while closed", async () => {
    globalThis.fetch = mock(
      async () =>
        new Response(JSON.stringify({ scope: "repo", groups: [] }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ) as unknown as typeof fetch;

    renderDrawer(false);
    await flush();

    const drawer = document.querySelector(".nav-drawer");
    expect(drawer).not.toBeNull();
    expect(drawer).not.toHaveAttribute("data-open");
    expect(drawer).toHaveAttribute("aria-hidden", "true");
    expect(screen.getByText("Browse")).toBeInTheDocument();
  });
});
