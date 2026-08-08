// BusRail — /bus/sessions rendering: a member whose transcript was found
// is a clickable session button, one whose transcript was not found shows
// a "no transcript" placeholder instead.
import { describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { BusRail } from "./BusRail";

function mockFetchOnce(body: unknown, status = 200) {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
  ) as unknown as typeof fetch;
}

describe("BusRail", () => {
  test("a found transcript renders as a button; a missing one shows a not-found placeholder", async () => {
    mockFetchOnce({
      sessions: [
        {
          name: "alice",
          kind: "human",
          session: "sess-a",
          stale: false,
          transcript: { found: true, path: "/x/sess-a.jsonl", mtime: 1000, size: 10 },
        },
        {
          name: "bob",
          kind: "agent",
          session: "sess-b",
          stale: false,
          transcript: { found: false },
        },
      ],
    });

    render(
      <MemoryRouter initialEntries={["/bus?room=exp"]}>
        <BusRail />
      </MemoryRouter>,
    );

    await waitFor(() => expect(screen.getByText("sess-a")).toBeInTheDocument());
    expect(screen.getByText("sess-a").closest("button")).not.toBeNull();

    expect(screen.getByText("sess-b").closest("button")).toBeNull();
    expect(screen.getByText("no transcript")).toBeInTheDocument();
  });
});
