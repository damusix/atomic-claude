import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createMemoryRouter, RouterProvider } from "react-router";
import { ApiProvider } from "../../utils/api";
import { events } from "../../utils/events";
import { __resetForTest } from "../../utils/memberStore";
import { SchemaView } from "./SchemaView";

const NAV_FIXTURE = { scope: "realm", name: "acme", branch: "", groups: [] };

function seedMemberCookie(member: string) {
  document.cookie = `atomic-member=${encodeURIComponent(JSON.stringify({ "realm:acme": member }))}; path=/`;
}

function requestURL(input: RequestInfo | URL): string {
  return typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
}

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

// routes[urlSubstring] -> response body (or a function of the URL, for
// tests that need per-member schema payloads keyed on ?member=). "/nav" is
// always stubbed so the member store resolves without each test wiring it.
function mockFetch(routes: Record<string, unknown | ((url: string) => unknown)>, missingStatus = 500) {
  const withNav = { "/nav": NAV_FIXTURE, ...routes };
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = requestURL(input);
    for (const [needle, body] of Object.entries(withNav)) {
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
    __resetForTest();
    document.cookie = "atomic-member=; path=/; max-age=0";
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
    // Sections are the directory the objects were declared in, named as
    // written; a file at the root has no directory to name.
    expect(screen.getByText("(no directory)")).toBeInTheDocument();
    expect(screen.queryByText("Views")).not.toBeInTheDocument();
    expect(screen.getByText("id")).toBeInTheDocument();
    expect(screen.getByText("orders")).toBeInTheDocument();
    expect(screen.getByText("insert_user")).toBeInTheDocument();
    expect(screen.queryByLabelText("Code member")).not.toBeInTheDocument();
  });

  // Filtering matches column names as well as table names: "which table has
  // a tax_year column" is as common a question as "where is the invoice
  // table", and only one of them is answerable by table name.
  test("filter matches on table name and on column name", async () => {
    mockFetch({
      "/code/graph/members": { members: [{ prefix: "", indexed: true }] },
      "/code/schema": {
        tables: [
          {
            node: { id: "t1", name: "invoice", kind: "table", filePath: "sql/01_tables/a.sql", startLine: 1 },
            columns: [{ id: "c1", name: "tax_year", kind: "column", filePath: "sql/01_tables/a.sql", startLine: 2 }],
            fkSources: [],
            writers: [],
          },
          {
            node: { id: "t2", name: "party", kind: "table", filePath: "sql/01_tables/b.sql", startLine: 1 },
            columns: [{ id: "c2", name: "label", kind: "column", filePath: "sql/01_tables/b.sql", startLine: 2 }],
            fkSources: [],
            writers: [],
          },
        ],
      },
    });

    renderSchema();

    // Scoped to the card headings' links: the heading also carries a kind
    // badge, and the object names appear again in the rail index.
    const cards = () =>
      [...document.querySelectorAll(".code-schema-main .code-schema-table-name .code-node-link")].map(
        (e) => e.textContent,
      );

    await waitFor(() => expect(cards()).toContain("invoice"));

    const filter = screen.getByPlaceholderText("Table or column name…");

    await userEvent.type(filter, "party");
    await waitFor(() => expect(cards()).not.toContain("invoice"));
    expect(cards()).toContain("party");

    await userEvent.clear(filter);
    await userEvent.type(filter, "tax_year");
    await waitFor(() => expect(cards()).toContain("invoice"));
    expect(cards()).not.toContain("party");
  });

  // Most projects have no SQL, so an empty schema is the normal case rather
  // than a fault — it says so, and routes to the surfaces that do cover
  // non-SQL code instead of leaving a blank page.
  test("empty schema explains itself and points at the code surfaces", async () => {
    mockFetch({
      "/code/graph/members": { members: [{ prefix: "", indexed: true }] },
      "/code/schema": { tables: [] },
    });

    renderSchema();

    await waitFor(() => expect(screen.getByText("No SQL in this index")).toBeInTheDocument());
    expect(screen.getByText(/it is not an error/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /code graph/ })).toHaveAttribute(
      "href",
      "/graph?view=code",
    );
    expect(screen.getByRole("link", { name: /code search/ })).toHaveAttribute("href", "/search");
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
    const select = screen.getByLabelText("Code member");
    expect(select).toBeInTheDocument();

    await userEvent.selectOptions(select, "beta");

    await waitFor(() => expect(screen.getByText("beta_table")).toBeInTheDocument());
    expect(document.cookie).toContain(encodeURIComponent(JSON.stringify({ "realm:acme": "beta" })));
  });

  test("the empty-prefix member renders the realm's name from the store", async () => {
    mockFetch({
      "/code/graph/members": {
        members: [
          { prefix: "", indexed: true },
          { prefix: "atomic", indexed: true },
        ],
      },
      "/code/schema": { tables: [] },
    });

    renderSchema();

    const select = await screen.findByLabelText("Code member");
    expect(select.querySelector("option[value='']")).toHaveTextContent("acme");
  });

  test("a stored member absent from Schema's member list renders the first member and leaves the cookie unchanged", async () => {
    seedMemberCookie("bogus");
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
    expect(document.cookie).toContain(encodeURIComponent(JSON.stringify({ "realm:acme": "bogus" })));
  });

  // The rail index is published from here, and the fetch hook holds `data`
  // null in flight and nulls it again on a failed refetch. Publishing
  // regardless emptied the rail on every member switch and every transient
  // failure, which read as the index vanishing at random.
  test("never publishes an empty index while the response is not loaded", async () => {
    const published: number[] = [];
    const off = events.on("schema.index", ({ sections }) => published.push(sections.length));

    mockFetch({
      "/code/graph/members": { members: [{ prefix: "", indexed: true }] },
      "/code/schema": {
        tables: [
          {
            node: { id: "t1", name: "invoice", kind: "table", filePath: "sql/a.sql", startLine: 1 },
            columns: [],
            fkSources: [],
            writers: [],
          },
        ],
      },
    });

    const { unmount } = renderSchema();
    await waitFor(() => expect(screen.getByText("invoice")).toBeInTheDocument());

    // Every publish before unmount describes a loaded response.
    expect(published.length).toBeGreaterThan(0);
    expect(published.every((n) => n > 0)).toBe(true);

    // Leaving the route does clear it — a stale tree beside another page
    // would be worse than none.
    unmount();
    expect(published.at(-1)).toBe(0);
    off();
  });
});
