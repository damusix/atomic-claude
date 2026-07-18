import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router";
import { ApiProvider } from "../../utils/api";
import { SchemaView } from "./SchemaView";

function requestURL(input: RequestInfo | URL): string {
  return typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

// routes[urlSubstring] -> response body (or a function of the URL, for
// tests that need per-member schema payloads keyed on ?member=).
function mockFetch(routes: Record<string, unknown | ((url: string) => unknown)>, missingStatus = 500) {
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = requestURL(input);
    for (const [needle, body] of Object.entries(routes)) {
      if (url.includes(needle)) {
        const resolved = typeof body === "function" ? (body as (u: string) => unknown)(url) : body;
        return jsonResponse(resolved);
      }
    }
    return jsonResponse({ error: "not found" }, missingStatus);
  }) as unknown as typeof fetch;
}

function renderSchema() {
  const router = createMemoryRouter([{ path: "/code/schema", element: <SchemaView /> }], {
    initialEntries: ["/code/schema"],
  });
  return render(
    <ApiProvider>
      <RouterProvider router={router} />
    </ApiProvider>,
  );
}

describe("SchemaView", () => {
  afterEach(() => {
    mock.restore();
    // @ts-expect-error — test-only global cleanup
    delete globalThis.fetch;
  });

  test("renders tables with columns, FK sources, and writers; no member picker for a single member", async () => {
    mockFetch({
      "/code/graph/members": { members: [{ prefix: "", indexed: true }] },
      "/code/schema": {
        tables: [
          {
            node: { id: "tbl-users", name: "users", kind: "table", filePath: "schema.sql", startLine: 1 },
            columns: [{ id: "col-id", name: "id", kind: "column", filePath: "schema.sql", startLine: 2 }],
            fkSources: [
              { id: "tbl-orders", name: "orders", kind: "table", filePath: "schema.sql", startLine: 10 },
            ],
            writers: [
              { id: "proc-insert", name: "insert_user", kind: "procedure", filePath: "schema.sql", startLine: 20 },
            ],
          },
        ],
      },
    });

    renderSchema();

    await waitFor(() => expect(screen.getByText("users")).toBeInTheDocument());
    expect(screen.getByText("Tables")).toBeInTheDocument();
    expect(screen.queryByText("Views")).not.toBeInTheDocument();
    expect(screen.getByText("id")).toBeInTheDocument();
    expect(screen.getByText("orders")).toBeInTheDocument();
    expect(screen.getByText("insert_user")).toBeInTheDocument();
    expect(screen.queryByLabelText("Code member")).not.toBeInTheDocument();
  });

  test("empty schema renders the empty-index message", async () => {
    mockFetch({
      "/code/graph/members": { members: [{ prefix: "", indexed: true }] },
      "/code/schema": { tables: [] },
    });

    renderSchema();

    await waitFor(() => expect(screen.getByText("No SQL schema found in this index.")).toBeInTheDocument());
  });

  test("not-indexed member: error envelope renders a schema-unavailable message", async () => {
    mockFetch(
      {
        "/code/graph/members": { members: [{ prefix: "cold", indexed: false }] },
      },
      500,
    );

    renderSchema();

    await waitFor(() =>
      expect(screen.getByText("Schema unavailable — is this member indexed?")).toBeInTheDocument(),
    );
  });

  test("multi-member realm shows a picker and refetches on selection", async () => {
    mockFetch({
      "/code/graph/members": {
        members: [
          { prefix: "alpha", indexed: true },
          { prefix: "beta", indexed: true },
        ],
      },
      "/code/schema": (url: string) => {
        const member = new URL(url).searchParams.get("member");
        return {
          tables: [
            {
              node: { id: `tbl-${member}`, name: `${member}_table`, kind: "table", filePath: "s.sql", startLine: 1 },
              columns: [],
              fkSources: [],
              writers: [],
            },
          ],
        };
      },
    });

    renderSchema();

    await waitFor(() => expect(screen.getByText("alpha_table")).toBeInTheDocument());
    expect(screen.getByLabelText("Code member")).toBeInTheDocument();
  });
});
