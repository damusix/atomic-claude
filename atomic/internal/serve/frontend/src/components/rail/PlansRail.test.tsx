import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import type { PlanRow } from "../../utils/plansApi";
import { PlansRail } from "./PlansRail";

const ROW: PlanRow = {
  slug: "atomic-doctor",
  title: "atomic-doctor",
  description: "Verifies install and project-state coherence.",
  docs: [
    {
      path: "docs/spec/atomic-doctor.md",
      versions: [
        {
          sha: "sha-main",
          label: "main",
          isMain: true,
          mtime: "2026-08-19T00:00:00Z",
          checkouts: [{ id: "w-main", branch: "main", path: ".", outsideRoot: false, isMain: true, fileMtime: "2026-08-19T00:00:00Z" }],
        },
      ],
    },
  ],
  bundles: [],
  dotCount: 1,
  dotMerged: true,
};

function mockFetchByUrl(handlers: Record<string, unknown>) {
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    for (const [match, body] of Object.entries(handlers)) {
      if (url.includes(match)) {
        return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
      }
    }
    return new Response(JSON.stringify({ error: "unexpected path: " + url }), { status: 404 });
  }) as unknown as typeof fetch;
}

function renderRail(initialPath: string) {
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <PlansRail />
    </MemoryRouter>,
  );
}

describe("PlansRail", () => {
  afterEach(() => {
    mock.restore();
    // @ts-expect-error — test-only global cleanup
    delete globalThis.fetch;
  });

  test("carries ?member= into its own /api/plans fetch", async () => {
    mockFetchByUrl({ "/plans": [ROW] });
    renderRail("/plans/atomic-doctor/docs/spec/atomic-doctor.md?member=server");

    await waitFor(() => expect(screen.getByText("spec.md")).toBeInTheDocument());

    const fetchMock = globalThis.fetch as unknown as { mock: { calls: [RequestInfo | URL][] } };
    const calledUrl = fetchMock.mock.calls[0][0];
    const url = typeof calledUrl === "string" ? calledUrl : calledUrl.toString();
    expect(url).toContain("member=server");
  });

  test("renders no Links or Graph tabs", async () => {
    mockFetchByUrl({ "/plans": [ROW] });
    renderRail("/plans/atomic-doctor/docs/spec/atomic-doctor.md");

    await waitFor(() => expect(screen.getByText("spec.md")).toBeInTheDocument());

    expect(screen.queryByText("Links")).not.toBeInTheDocument();
    expect(screen.queryByText("Graph")).not.toBeInTheDocument();
    expect(screen.queryByText("Bundle", { selector: "button, [role=tab]" })).not.toBeInTheDocument();
  });
});
