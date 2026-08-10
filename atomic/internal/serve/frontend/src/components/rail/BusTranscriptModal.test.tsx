// BusTranscriptModal — the entry range shown in the header comes straight
// from the /api/bus/transcript response's firstEntry/lastEntry (server
// single-source, see api_bus_transcript.go's transcriptMeta) rather than
// being recomputed from shownEntries/totalEntries/offset here.
import { describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { BusTranscriptModal } from "./BusTranscriptModal";
import type { BusSessionInfo } from "./BusRail";

function mockFetchOnce(body: unknown, status = 200) {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
  ) as unknown as typeof fetch;
}

const MEMBER: BusSessionInfo = {
  name: "alice",
  kind: "human",
  session: "sess-a",
  stale: false,
  transcript: { found: true, path: "/x/sess-a.jsonl" },
};

describe("BusTranscriptModal", () => {
  test("shows the server-computed entry range and pages by offset", async () => {
    mockFetchOnce({
      html: "<p>hi</p>",
      title: "Fix the bug",
      path: "/x/sess-a.jsonl",
      // shownEntries deliberately does not agree with firstEntry/lastEntry
      // under the header's old (removed) recompute formula — pinning that
      // the header renders the server's firstEntry/lastEntry directly
      // rather than deriving its own range from shownEntries/totalEntries.
      shownEntries: 50,
      totalEntries: 409,
      offset: 0,
      firstEntry: 310,
      lastEntry: 409,
    });

    render(<BusTranscriptModal member={MEMBER} onClose={() => {}} />);

    // Newest-first display: the range reads latest→oldest, and the pager
    // leads with newer (disabled on the latest window) then older.
    await waitFor(() => expect(screen.getByText(/entries 409–310 of 409/)).toBeInTheDocument());
    expect(screen.getByLabelText("Older entries")).not.toBeDisabled();
    expect(screen.getByLabelText("Newer entries")).toBeDisabled();
    const buttons = screen.getAllByRole("button", { name: /entries/ });
    expect(buttons[0]).toHaveAttribute("aria-label", "Newer entries");
    expect(buttons[1]).toHaveAttribute("aria-label", "Older entries");
  });
});
