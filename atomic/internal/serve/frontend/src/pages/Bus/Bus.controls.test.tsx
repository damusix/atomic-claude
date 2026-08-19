// The two destructive room controls. Both stop listeners that cannot be
// restarted from this page, so what matters is that each one asks first and
// that declining sends nothing.
import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { Bus } from "./Bus";

interface Posted {
  url: string;
  body: unknown;
}

// mockBusFetch answers each /api/bus/* route with a fixture and records every
// POST, so a test can assert on what the page actually sent.
function mockBusFetch(): Posted[] {
  const posted: Posted[] = [];

  globalThis.fetch = mock(async (input: unknown, init?: RequestInit) => {
    const url = String(input);
    const json = (body: unknown) =>
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });

    if (init?.method === "POST") {
      posted.push({ url, body: JSON.parse(String(init.body)) });
      return json({ ok: true });
    }
    if (url.includes("/bus/status")) return json({ running: true, name: "web" });
    if (url.includes("/bus/rooms")) return json({ running: true, rooms: [{ name: "serve", members: 2 }] });
    if (url.includes("/bus/who")) {
      return json({
        halted: false,
        members: [
          { name: "web", kind: "human", stale: false },
          { name: "agent-a", kind: "agent", stale: false },
        ],
      });
    }
    if (url.includes("/bus/log")) return json({ envelopes: [] });
    return json({});
  }) as unknown as typeof fetch;

  return posted;
}

function renderRoom() {
  const router = createMemoryRouter([{ path: "/bus", element: <Bus /> }], {
    initialEntries: ["/bus?room=serve"],
  });
  return render(<RouterProvider router={router} />);
}

describe("Message Bus room controls", () => {
  afterEach(() => {
    mock.restore();
  });

  test("ending a member's session posts that member's name once confirmed", async () => {
    const posted = mockBusFetch();
    window.confirm = mock(() => true) as unknown as typeof window.confirm;

    renderRoom();

    const end = await screen.findByRole("button", { name: "End agent-a's session" });
    await userEvent.click(end);

    await waitFor(() => expect(posted.some((p) => p.url.includes("/bus/end"))).toBe(true));
    expect(posted.find((p) => p.url.includes("/bus/end"))?.body).toEqual({
      room: "serve",
      name: "agent-a",
    });
  });

  test("declining the prompt sends nothing", async () => {
    const posted = mockBusFetch();
    window.confirm = mock(() => false) as unknown as typeof window.confirm;

    renderRoom();

    const end = await screen.findByRole("button", { name: "End agent-a's session" });
    await userEvent.click(end);

    expect(posted.some((p) => p.url.includes("/bus/end"))).toBe(false);
  });

  // The operator is the one clicking; offering to cut their own listener is a
  // footgun with no use, so the roster shows no control against their name.
  test("the operator's own chip carries no end control", async () => {
    mockBusFetch();
    window.confirm = mock(() => true) as unknown as typeof window.confirm;

    renderRoom();

    await screen.findByRole("button", { name: "End agent-a's session" });
    expect(screen.queryByRole("button", { name: "End web's session" })).toBeNull();
  });

  test("closing the room posts the room once confirmed", async () => {
    const posted = mockBusFetch();
    window.confirm = mock(() => true) as unknown as typeof window.confirm;

    renderRoom();

    const close = await screen.findByRole("button", { name: "Close" });
    await userEvent.click(close);

    await waitFor(() => expect(posted.some((p) => p.url.includes("/bus/close"))).toBe(true));
    expect(posted.find((p) => p.url.includes("/bus/close"))?.body).toEqual({ room: "serve" });
  });
});
