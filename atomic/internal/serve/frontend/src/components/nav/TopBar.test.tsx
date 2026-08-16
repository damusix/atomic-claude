import { afterEach, describe, expect, test } from "bun:test";
import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { TopBar } from "./TopBar";

describe("TopBar", () => {
  afterEach(() => {
    window.localStorage.clear();
    document.documentElement.removeAttribute("data-theme");
  });

  test("renders a stubbed live-reload connection state", () => {
    render(
      <MemoryRouter>
        <TopBar connState="live" />
      </MemoryRouter>,
    );

    const indicator = document.querySelector(".conn-indicator");
    expect(indicator).toHaveAttribute("data-conn-state", "live");
    expect(screen.getByText("live")).toBeInTheDocument();
  });

  test("defaults to reconnecting when no connState is stubbed", () => {
    render(
      <MemoryRouter>
        <TopBar />
      </MemoryRouter>,
    );

    const indicator = document.querySelector(".conn-indicator");
    expect(indicator).toHaveAttribute("data-conn-state", "reconnecting");
  });

  test("renders the breadcrumb page label from the current route", () => {
    render(
      <MemoryRouter initialEntries={["/page/wiki/index.md"]}>
        <TopBar />
      </MemoryRouter>,
    );

    expect(document.querySelector(".breadcrumb-page")).toHaveTextContent("index.md");
  });

  test("renders every path segment, not just the leaf", () => {
    render(
      <MemoryRouter initialEntries={["/page/docs/wiki/serve.md"]}>
        <TopBar />
      </MemoryRouter>,
    );

    const crumbs = [...document.querySelectorAll(".breadcrumb-folder")].map((e) => e.textContent);
    expect(crumbs).toEqual(["docs", "wiki"]);
    expect(document.querySelector(".breadcrumb-page")).toHaveTextContent("serve.md");
  });

  // Modes and theme moved to components/nav/IconRail so a mode is switched in
  // exactly one place — the header must not grow a second control cluster.
  test("carries no view-mode or theme controls", () => {
    render(
      <MemoryRouter>
        <TopBar />
      </MemoryRouter>,
    );

    expect(document.getElementById("btn-graph")).toBeNull();
    expect(document.getElementById("btn-bus")).toBeNull();
    expect(document.querySelector(".theme-toggle")).toBeNull();
  });
});
