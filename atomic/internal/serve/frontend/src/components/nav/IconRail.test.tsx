import { afterEach, describe, expect, mock, test } from "bun:test";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { IconRail } from "./IconRail";

/** Answers /api/code/capabilities; everything else 404s. */
function mockCapabilities(schema: boolean) {
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input instanceof URL ? input.toString() : input.url;
    if (url.includes("/code/capabilities")) {
      return new Response(JSON.stringify({ schema, source: "detected" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      });
    }
    return new Response(JSON.stringify({ error: "not found" }), { status: 404 });
  }) as unknown as typeof fetch;
}

function renderAt(pathname: string, props: Partial<Parameters<typeof IconRail>[0]> = {}) {
  return render(
    <MemoryRouter initialEntries={[pathname]}>
      <IconRail navOpen={false} onToggleNav={() => {}} {...props} />
    </MemoryRouter>,
  );
}

describe("IconRail", () => {
  afterEach(() => {
    mock.restore();
    // @ts-expect-error — test-only global cleanup
    delete globalThis.fetch;
  });

  test("offers every unconditional view mode as a route", () => {
    mockCapabilities(false);
    renderAt("/");

    expect(screen.getByRole("link", { name: "Docs" })).toHaveAttribute("href", "/");
    expect(screen.getByRole("link", { name: "Graph" })).toHaveAttribute("href", "/graph");
    expect(screen.getByRole("link", { name: "Message Bus" })).toHaveAttribute("href", "/bus");
  });

  // Most repositories have no SQL, and a permanent mode that can only ever say
  // "nothing here" is a promise the tool cannot keep.
  test("Schema mode is absent when the index holds no SQL", async () => {
    mockCapabilities(false);
    renderAt("/");

    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalled());
    expect(screen.queryByRole("link", { name: "Schema" })).not.toBeInTheDocument();
  });

  test("Schema mode appears once the server reports SQL objects", async () => {
    mockCapabilities(true);
    renderAt("/");

    await waitFor(() =>
      expect(screen.getByRole("link", { name: "Schema" })).toHaveAttribute("href", "/code/schema"),
    );
  });

  // Removing the icon for the page someone is looking at would strand them
  // there with no way back to it.
  test("Schema mode stays while it is the open route, whatever the probe says", async () => {
    mockCapabilities(false);
    renderAt("/code/schema");

    await waitFor(() => expect(globalThis.fetch).toHaveBeenCalled());
    expect(screen.getByRole("link", { name: "Schema" })).toHaveAttribute("aria-current", "page");
  });

  // A mode owns more than one route: reading any page is still Docs mode.
  // Marking active by exact pathname would leave the rail showing no active
  // mode on most of the app.
  test("marks Docs active while reading any page, not just the landing route", () => {
    mockCapabilities(false);
    renderAt("/page/docs/wiki/serve.md");

    expect(screen.getByRole("link", { name: "Docs" })).toHaveAttribute("aria-current", "page");
    expect(screen.getByRole("link", { name: "Graph" })).not.toHaveAttribute("aria-current");
  });

  test("Browse reports its pressed state and reaches the shell's toggle", async () => {
    mockCapabilities(false);
    const onToggleNav = mock(() => {});
    renderAt("/", { navOpen: true, onToggleNav });

    const browse = screen.getByRole("button", { name: /Browse/ });
    expect(browse).toHaveAttribute("aria-pressed", "true");

    await userEvent.click(browse);
    expect(onToggleNav).toHaveBeenCalledTimes(1);
  });
});
