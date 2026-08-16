// Two components asking the shared FetchEngine for the same URL at the same
// moment must both end up with the data. /api/nav has exactly two consumers
// (the top bar's scope chip and the nav tree), and when only one of them
// received the deduped response the chip silently rendered nothing.
import { describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { useApi } from "./api";

function Consumer({ label }: { label: string }) {
  const { get } = useApi();
  const { data } = get<{ name: string }>("/nav");
  return <span data-testid={label}>{data ? data.name : "—"}</span>;
}

describe("shared FetchEngine dedupe", () => {
  test("both simultaneous subscribers to one URL receive the payload", async () => {
    globalThis.fetch = mock(
      async () =>
        new Response(JSON.stringify({ name: "taxgentic" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    ) as unknown as typeof fetch;

    render(
      <>
        <Consumer label="a" />
        <Consumer label="b" />
      </>,
    );

    await waitFor(() => expect(screen.getByTestId("a")).toHaveTextContent("taxgentic"));
    await waitFor(() => expect(screen.getByTestId("b")).toHaveTextContent("taxgentic"));
  });
});
