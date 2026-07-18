import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor, within } from "@testing-library/react";
import { ApiProvider } from "../../utils/api";
import { events } from "../../utils/events";
import { Rail } from "./Rail";
import type { RailResponse } from "./types";

const RAIL_FIXTURE: RailResponse = {
  relpath: "wiki/index.md",
  orphan: false,
  properties: [
    { key: "type", value: "Repo", isURL: false, isJSON: false },
    { key: "resource", value: "https://example.com", isURL: true, isJSON: false },
    { key: "tags", value: '["a","b"]', isURL: false, isJSON: true },
  ],
  out: [
    {
      target: "notes.md",
      resolvedPath: "notes.md",
      broken: false,
      ambiguous: false,
      codeFile: false,
      external: false,
    },
    {
      target: "gone.md",
      resolvedPath: "",
      broken: true,
      ambiguous: false,
      codeFile: false,
      external: false,
    },
    {
      target: "https://example.com",
      resolvedPath: "",
      broken: false,
      ambiguous: false,
      codeFile: false,
      external: true,
    },
    {
      target: "render.go",
      resolvedPath: "atomic/internal/serve/render.go",
      broken: false,
      ambiguous: false,
      codeFile: true,
      external: false,
    },
  ],
  in: [{ path: "other.md" }],
  graphDataURL: "/graph/data?node=wiki/index.md&depth=1",
};

function mockFetchOnce(body: unknown, status = 200) {
  globalThis.fetch = mock(
    async () =>
      new Response(JSON.stringify(body), {
        status,
        headers: { "Content-Type": "application/json" },
      }),
  ) as unknown as typeof fetch;
}

describe("Rail", () => {
  afterEach(() => {
    mock.restore();
  });

  test("renders nothing but the bare aside until page.resolved fires", () => {
    render(
      <ApiProvider>
        <Rail />
      </ApiProvider>,
    );
    const aside = document.getElementById("right-rail");
    expect(aside).not.toBeNull();
    expect(aside?.querySelector("#rail-props")).toBeNull();
  });

  test("fetches /api/rail/<relpath> on page.resolved and renders Properties/OUT/IN panels", async () => {
    mockFetchOnce(RAIL_FIXTURE);
    render(
      <ApiProvider>
        <Rail />
      </ApiProvider>,
    );

    events.emit("page.resolved", { relpath: "wiki/index.md" });

    await waitFor(() => expect(document.querySelector("#rail-props")).not.toBeNull());

    // Properties: plain, isURL anchor, isJSON pretty-printed block.
    const props = within(document.getElementById("rail-props-content") as HTMLElement);
    expect(props.getByText("type")).toBeInTheDocument();
    expect(props.getByText("Repo")).toBeInTheDocument();
    const urlAnchor = props.getByText("https://example.com") as HTMLAnchorElement;
    expect(urlAnchor.tagName).toBe("A");
    expect(urlAnchor.target).toBe("_blank");
    expect(document.querySelector(".rail-prop-json")).not.toBeNull();

    // OUT: resolved link, broken span, external new-tab, codeFile /file/ link.
    const out = within(document.getElementById("rail-out-content") as HTMLElement);
    expect(out.getByText("notes.md").closest("a")).toHaveAttribute("href", "/page/notes.md");
    expect(out.getByText("gone.md")).toHaveClass("wikilink-broken");
    expect(out.getByText("render.go").closest("a")).toHaveAttribute(
      "href",
      "/file/atomic/internal/serve/render.go",
    );

    // IN backlinks.
    const inLinks = within(document.getElementById("rail-in-content") as HTMLElement);
    expect(inLinks.getByText("other.md").closest("a")).toHaveAttribute("href", "/page/other.md");
  });

  test("clears the rail (hides panels) when page.resolved carries a null relpath", async () => {
    mockFetchOnce(RAIL_FIXTURE);
    render(
      <ApiProvider>
        <Rail />
      </ApiProvider>,
    );

    events.emit("page.resolved", { relpath: "wiki/index.md" });
    await waitFor(() => expect(document.querySelector("#rail-props")).not.toBeNull());

    events.emit("page.resolved", { relpath: null });
    await waitFor(() => expect(document.querySelector("#rail-props")).toBeNull());
  });

  test("a page with no graph membership (404 from /api/rail) shows the bare aside, not an error", async () => {
    mockFetchOnce({ error: "not found" }, 404);
    render(
      <ApiProvider>
        <Rail />
      </ApiProvider>,
    );

    events.emit("page.resolved", { relpath: "orphan.md" });

    // Give the 404 a beat to land, then assert it stays that way — a
    // fixed-tick setTimeout races the attempt()/FetchEngine promise chain.
    await new Promise((r) => setTimeout(r, 20));
    await waitFor(() => expect(document.querySelector("#rail-props")).toBeNull());
  });
});
