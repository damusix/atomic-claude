import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { registerRailCy } from "../components/rail/railCytoscapeStyle";
import { events } from "../utils/events";
import { useTheme } from "./useTheme";

const STORAGE_KEY = "atomic-theme";

function ThemeProbe() {
  const { theme, toggle } = useTheme();
  return (
    <button type="button" onClick={toggle}>
      theme:{theme}
    </button>
  );
}

describe("useTheme", () => {
  beforeEach(() => {
    window.localStorage.clear();
    document.documentElement.removeAttribute("data-theme");
  });

  afterEach(() => {
    window.localStorage.clear();
    document.documentElement.removeAttribute("data-theme");
  });

  test("reads a persisted choice over the OS preference", () => {
    window.localStorage.setItem(STORAGE_KEY, "dark");
    render(<ThemeProbe />);
    expect(screen.getByRole("button")).toHaveTextContent("theme:dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  test("falls back to the OS preference when nothing is persisted", () => {
    const matchMediaMock = mock((query: string) => ({
      matches: query.includes("dark"),
      media: query,
      addEventListener: () => {},
      removeEventListener: () => {},
    }));
    // @ts-expect-error — partial MediaQueryList stub, enough for useTheme's read.
    window.matchMedia = matchMediaMock;

    render(<ThemeProbe />);
    expect(screen.getByRole("button")).toHaveTextContent("theme:dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
  });

  test("toggle flips the theme, persists it, and emits theme.changed on the event bus", async () => {
    window.localStorage.setItem(STORAGE_KEY, "light");
    const seen: string[] = [];
    const off = events.on("theme.changed", (payload) => seen.push(payload.theme));

    render(<ThemeProbe />);
    const user = userEvent.setup();
    await act(async () => {
      await user.click(screen.getByRole("button"));
    });

    expect(screen.getByRole("button")).toHaveTextContent("theme:dark");
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(window.localStorage.getItem(STORAGE_KEY)).toBe("dark");
    expect(seen).toEqual(["dark"]);

    off();
  });

  test("toggle fires the retheme cascade — rail Cytoscape, carried graph engine, mermaid", async () => {
    window.localStorage.setItem(STORAGE_KEY, "light");

    const cyStyle = mock(() => {});
    registerRailCy({ style: cyStyle }, "wiki/index.md");

    const graphRetheme = mock(() => {});
    window.GraphCore = { mount: mock(() => {}), teardown: mock(() => {}), retheme: graphRetheme };

    const mermaidRun = mock(() => {});
    (window as unknown as { mermaid: { initialize: () => void; run: typeof mermaidRun } }).mermaid = {
      initialize: () => {},
      run: mermaidRun,
    };
    const pre = document.createElement("pre");
    pre.className = "mermaid";
    pre.dataset.mermaidSrc = "graph TD; A-->B;";
    document.body.appendChild(pre);

    render(<ThemeProbe />);
    const user = userEvent.setup();
    await act(async () => {
      await user.click(screen.getByRole("button"));
    });

    expect(cyStyle).toHaveBeenCalledTimes(1);
    expect(graphRetheme).toHaveBeenCalledTimes(1);
    await act(async () => {}); // flush rethemeMermaid's async attempt()
    expect(mermaidRun).toHaveBeenCalledTimes(1);

    delete window.GraphCore;
    delete window.__railCy;
    delete (window as { mermaid?: unknown }).mermaid;
    document.body.innerHTML = "";
  });
});
