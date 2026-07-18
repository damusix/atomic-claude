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

  test("renders a #btn-graph link routing to /graph", () => {
    render(
      <MemoryRouter>
        <TopBar />
      </MemoryRouter>,
    );

    const btn = document.getElementById("btn-graph");
    expect(btn).toBeInTheDocument();
    expect(btn).toHaveAttribute("href", "/graph");
    expect(btn).toHaveAttribute("aria-label", "Network view — toggle full graph");
  });
});
