import { afterEach, describe, expect, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { Search } from "./Search";

// A minimal EventSource stand-in shared with useSearchStream.test.ts's
// pattern — captures listeners per event so the test can dispatch SSE
// frames directly.
class FakeEventSource {
  static instances: FakeEventSource[] = [];
  url: string;
  private listeners: Record<string, ((e: { data: string }) => void)[]> = {};

  constructor(url: string) {
    this.url = url;
    FakeEventSource.instances.push(this);
  }

  addEventListener(event: string, cb: (e: { data: string }) => void) {
    (this.listeners[event] ??= []).push(cb);
  }

  dispatch(event: string, data: unknown) {
    for (const cb of this.listeners[event] ?? []) cb({ data: JSON.stringify(data) });
  }

  close() {}
}

function renderAt(path: string) {
  const router = createMemoryRouter([{ path: "/search", element: <Search /> }], { initialEntries: [path] });
  render(<RouterProvider router={router} />);
  return router;
}

describe("Search page", () => {
  afterEach(() => {
    FakeEventSource.instances = [];
    // @ts-expect-error — test-only global stub
    delete globalThis.EventSource;
  });

  test("shows the search form when there is no query", () => {
    renderAt("/search");
    expect(screen.getByPlaceholderText("Search markdown or code…")).toBeInTheDocument();
  });

  test("streams md and code results per member, with a not-indexed note", async () => {
    // @ts-expect-error — test-only global stub
    globalThis.EventSource = FakeEventSource;
    renderAt("/search?q=auth&src=all");

    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));
    const es = FakeEventSource.instances[0]!;
    expect(es.url).toContain("q=auth");
    expect(es.url).toContain("src=all");

    expect(screen.getByText("Searching markdown…")).toBeInTheDocument();
    expect(screen.getByText("Searching code…")).toBeInTheDocument();

    es.dispatch("md", { query: "auth", truncated: false, cap: 50, results: [{ relpath: "wiki/auth.md", line: 3, snippet: "auth flow" }] });
    await waitFor(() => expect(screen.getByText("wiki/auth.md:3")).toBeInTheDocument());

    es.dispatch("code", { member: { key: "cold", prefix: "cold", indexed: false }, results: [] });
    es.dispatch("end", {});

    await waitFor(() => expect(screen.getByText(/not indexed/)).toBeInTheDocument());
    expect(screen.queryByText("Searching code…")).not.toBeInTheDocument();
  });

  test("clamps an unknown src param to 'all'", async () => {
    // @ts-expect-error — test-only global stub
    globalThis.EventSource = FakeEventSource;
    renderAt("/search?q=auth&src=bogus");

    await waitFor(() => expect(FakeEventSource.instances.length).toBe(1));
    // "all" tab shows both sections regardless of the bogus param.
    expect(screen.getByRole("heading", { name: "Markdown" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Code" })).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "All", selected: true })).toBeInTheDocument();
  });
});
