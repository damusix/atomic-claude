import { afterEach, describe, expect, mock, test } from "bun:test";
import { act, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { ApiProvider } from "../../utils/api";
import { events } from "../../utils/events";
import { NavTree } from "./NavTree";
import type { NavResponse } from "./types";

// CP2's shape (api_handlers.go apiNavResponse) — a realm-scope response with
// one stale repo and one bucket folder, exercising both leaf and branch
// nodes plus the stale badge.
const NAV_FIXTURE: NavResponse = {
  scope: "realm",
  name: "sample-realm",
  branch: "main",
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

  test("refetches /api/nav on every realm.changed — the nav-always leg of the live-reload reconcile", async () => {
    const fetchSpy = mock(
      async () =>
        new Response(JSON.stringify(NAV_FIXTURE), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
    );
    globalThis.fetch = fetchSpy as unknown as typeof fetch;

    render(
      <MemoryRouter>
        <ApiProvider>
          <NavTree />
        </ApiProvider>
      </MemoryRouter>,
    );

    await waitFor(() => expect(screen.getByText("index")).toBeInTheDocument());
    expect(fetchSpy).toHaveBeenCalledTimes(1);

    await act(async () => {
      events.emit("realm.changed", { fp: "fp2", changed: ["some/other.md"] });
    });

    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(2));
  });

  test("renders a group whose items field is null without crashing (repo-scope Code placeholder)", async () => {
    mockFetchOnce({
      scope: "repo",
      groups: [
        { name: "Docs", items: [{ label: "README", relpath: "README.md" }] },
        { name: "Code", items: null },
      ],
    });

    render(
      <MemoryRouter>
        <ApiProvider>
          <NavTree />
        </ApiProvider>
      </MemoryRouter>,
    );

    await waitFor(() => expect(screen.getByText("README")).toBeInTheDocument());
    expect(screen.getByText("Code")).toBeInTheDocument();
  });
});
