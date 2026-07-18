import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { ApiProvider } from "../../utils/api";
import { SearchPalette } from "./SearchPalette";
import type { ApiCodeSearchResponse, ApiMdSearchResponse } from "./types";

const MD_FIXTURE: ApiMdSearchResponse = {
  query: "auth",
  truncated: false,
  cap: 50,
  results: [{ relpath: "wiki/auth.md", line: 3, snippet: "auth flow" }],
};

const CODE_FIXTURE: ApiCodeSearchResponse = {
  members: [
    { key: "repo", prefix: "repo", indexed: true, results: [{ id: "n1", name: "Authenticate", kind: "func", filePath: "auth.go", startLine: 10 }] },
    { key: "cold", prefix: "cold", indexed: false, results: [] },
  ],
};

function mockFetchByUrl(handlers: Record<string, unknown>) {
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    for (const [match, body] of Object.entries(handlers)) {
      if (url.includes(match)) {
        return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
      }
    }
    return new Response(JSON.stringify({ error: "unexpected path" }), { status: 404 });
  }) as unknown as typeof fetch;
}

function renderPalette(open = true) {
  const onOpenChange = mock(() => {});
  render(
    <MemoryRouter>
      <ApiProvider>
        <SearchPalette open={open} onOpenChange={onOpenChange} />
      </ApiProvider>
    </MemoryRouter>,
  );
  return { onOpenChange };
}

describe("SearchPalette", () => {
  afterEach(() => {
    mock.restore();
  });

  test("⌘K opens the palette", async () => {
    const { onOpenChange } = renderPalette(false);
    await userEvent.keyboard("{Meta>}k{/Meta}");
    expect(onOpenChange).toHaveBeenCalledWith(true);
  });

  test("Escape closes an open palette", async () => {
    const { onOpenChange } = renderPalette(true);
    await userEvent.keyboard("{Escape}");
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  test("'/' opens the palette when focus isn't in a text field", async () => {
    const { onOpenChange } = renderPalette(false);
    await userEvent.keyboard("/");
    expect(onOpenChange).toHaveBeenCalledWith(true);
  });

  test("debounces typed input before fetching md results", async () => {
    mockFetchByUrl({ "/api/search/md": MD_FIXTURE });
    renderPalette(true);

    const input = screen.getByLabelText("Search");
    await userEvent.type(input, "auth");

    // Immediately after typing, no fetch should have happened yet.
    expect(globalThis.fetch).not.toHaveBeenCalled();

    await waitFor(() => expect(screen.getByText("wiki/auth.md")).toBeInTheDocument(), { timeout: 2000 });
    expect(globalThis.fetch).toHaveBeenCalledTimes(1);
  });

  test("md|code toggle switches the fetch target", async () => {
    mockFetchByUrl({ "/api/search/md": MD_FIXTURE, "/api/code/search": CODE_FIXTURE });
    renderPalette(true);

    const input = screen.getByLabelText("Search");
    await userEvent.type(input, "auth");
    await waitFor(() => expect(screen.getByText("wiki/auth.md")).toBeInTheDocument());

    await userEvent.click(screen.getByRole("button", { name: "code" }));
    await waitFor(() => expect(screen.getByText("Authenticate")).toBeInTheDocument());
    expect(screen.getByText(/not indexed/)).toBeInTheDocument();
  });

  test("selecting a markdown result navigates and closes the palette", async () => {
    mockFetchByUrl({ "/api/search/md": MD_FIXTURE });
    const { onOpenChange } = renderPalette(true);

    const input = screen.getByLabelText("Search");
    await userEvent.type(input, "auth");
    await waitFor(() => expect(screen.getByText("wiki/auth.md")).toBeInTheDocument());

    await userEvent.click(screen.getByText("wiki/auth.md"));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });
});
