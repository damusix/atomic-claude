// SchemaRail — the schema route's index, fed off the event bus by
// components/schema (the rail is mounted outside that Outlet subtree).
import { afterEach, describe, expect, test } from "bun:test";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { emitSchemaIndex } from "../../utils/events";
import { SchemaRail } from "./SchemaRail";

const INDEX = {
  member: "server",
  sections: [
    {
      id: "schema-sql-db-00-app-02-Tables",
      title: "App · Tables",
      count: 3,
      children: [
        { id: "schema-file-ingest", title: "Ingest", count: 1 },
        { id: "schema-file-billing", title: "Billing", count: 2 },
      ],
    },
  ],
};

afterEach(() => emitSchemaIndex({ member: "", sections: [] }));

describe("SchemaRail", () => {
  // The rail can mount either side of the schema data resolving; a plain
  // subscription would work for exactly one of those orders.
  test("renders an index emitted before it mounted", async () => {
    emitSchemaIndex(INDEX);
    render(<SchemaRail />);

    await waitFor(() => expect(screen.getByText("App · Tables")).toBeInTheDocument());
    expect(screen.getByText("Billing")).toBeInTheDocument();
    expect(screen.getByText("Ingest")).toBeInTheDocument();
  });

  test("renders an index emitted after it mounted", async () => {
    render(<SchemaRail />);
    expect(screen.getByText("nothing indexed")).toBeInTheDocument();

    emitSchemaIndex(INDEX);
    await waitFor(() => expect(screen.getByText("Billing")).toBeInTheDocument());
  });

  // Table names are already on screen in the grid; repeating all of them here
  // makes a longer list, not a way through one.
  test("lists directories and files, never individual tables", async () => {
    emitSchemaIndex(INDEX);
    render(<SchemaRail />);

    await waitFor(() => expect(screen.getByText("App · Tables")).toBeInTheDocument());
    const labels = [...document.querySelectorAll(".rail-schema-label")].map((e) => e.textContent);
    expect(labels).toEqual(["App · Tables", "Ingest", "Billing"]);
  });

  // A hash push would send the router looking for a route that does not exist.
  test("clicking an entry scrolls its section into view without navigating", async () => {
    emitSchemaIndex(INDEX);
    const target = document.createElement("div");
    target.id = "schema-file-billing";
    let scrolled = false;
    target.scrollIntoView = () => {
      scrolled = true;
    };
    document.body.appendChild(target);

    render(<SchemaRail />);
    await waitFor(() => expect(screen.getByText("Billing")).toBeInTheDocument());

    const click = new MouseEvent("click", { bubbles: true, cancelable: true });
    fireEvent(screen.getByText("Billing").closest("a")!, click);

    expect(scrolled).toBe(true);
    expect(click.defaultPrevented).toBe(true);
    target.remove();
  });
});
