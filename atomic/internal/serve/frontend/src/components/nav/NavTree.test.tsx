import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { ApiProvider } from "../../utils/api";
import { NavTree } from "./NavTree";
import type { NavResponse } from "./types";

// CP2's shape (api_handlers.go apiNavResponse) — a realm-scope response with
// one stale repo and one bucket folder, exercising both leaf and branch
// nodes plus the stale badge.
const NAV_FIXTURE: NavResponse = {
  scope: "realm",
  groups: [
    { name: "Realm", items: [{ label: "index", relpath: "wiki/index.md" }] },
    {
      name: "Repos",
      items: [
        { label: "atomic-claude", relpath: "wiki/repos/atomic-claude.md", stale: true },
        { label: "noorm", relpath: "wiki/repos/noorm.md" },
      ],
    },
    {
      name: "Buckets",
      items: [
        {
          label: "research",
          children: [{ label: "notes", relpath: "wiki/.buckets/research/notes.md" }],
        },
      ],
    },
    { name: "External", items: [{ label: "External links registry", relpath: "external" }] },
  ],
};

function mockFetchOnce(body: unknown, init: ResponseInit = {}) {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify(body), {
        status: 200,
        headers: { "Content-Type": "application/json" },
        ...init,
      }),
  ) as unknown as typeof fetch;
}

describe("NavTree", () => {
  afterEach(() => {
    mock.restore();
  });

  test("renders each group with its leaves and a stale badge on the stale repo", async () => {
    mockFetchOnce(NAV_FIXTURE);

    render(
      <MemoryRouter>
        <ApiProvider>
          <NavTree />
        </ApiProvider>
      </MemoryRouter>,
    );

    await waitFor(() => expect(screen.getByText("atomic-claude")).toBeInTheDocument());

    expect(screen.getByText("index")).toBeInTheDocument();
    expect(screen.getByText("noorm")).toBeInTheDocument();
    expect(screen.getByText("External links registry")).toBeInTheDocument();

    const staleRepo = screen.getByText("atomic-claude").closest("a");
    expect(staleRepo?.querySelector(".nav-badge-stale")).not.toBeNull();

    const freshRepo = screen.getByText("noorm").closest("a");
    expect(freshRepo?.querySelector(".nav-badge-stale")).toBeNull();
  });

  test("renders bucket folder nodes as collapsible branches", async () => {
    mockFetchOnce(NAV_FIXTURE);

    render(
      <MemoryRouter>
        <ApiProvider>
          <NavTree />
        </ApiProvider>
      </MemoryRouter>,
    );

    await waitFor(() => expect(screen.getByText("research")).toBeInTheDocument());
    expect(screen.getByText("research").closest('[data-part="branch-control"]')).not.toBeNull();
  });

  test("routes a leaf to its /page/<relpath> URL", async () => {
    mockFetchOnce(NAV_FIXTURE);

    render(
      <MemoryRouter>
        <ApiProvider>
          <NavTree />
        </ApiProvider>
      </MemoryRouter>,
    );

    await waitFor(() => expect(screen.getByText("index")).toBeInTheDocument());
    const link = screen.getByText("index").closest("a");
    expect(link).toHaveAttribute("href", "/page/wiki/index.md");

    const external = screen.getByText("External links registry").closest("a");
    expect(external).toHaveAttribute("href", "/external");
  });
});
