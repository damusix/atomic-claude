import { describe, expect, test } from "bun:test";
import { groupTables, tableMatches } from "./grouping";
import type { ApiTableSchema } from "./types";

function table(name: string, filePath: string, columns: string[] = []): ApiTableSchema {
  return {
    node: { id: `n:${filePath}:${name}`, name, kind: "table", filePath, startLine: 1 },
    columns: columns.map((c) => ({
      id: `c:${name}:${c}`,
      name: c,
      kind: "column",
      filePath,
      startLine: 1,
    })),
    fkSources: [],
    writers: [],
  };
}

describe("groupTables", () => {
  // Two levels and not one: grouping by directory alone put 108 of
  // taxgentic/server's 130 objects under a single heading, which is the flat
  // list the grouping was meant to break up.
  test("files are the sections inside a directory, not one bucket per directory", () => {
    const sections = groupTables([
      table("invoice", "sql/db/00_app/02_Tables/13_billing.sql"),
      table("payment", "sql/db/00_app/02_Tables/13_billing.sql"),
      table("source", "sql/db/00_app/02_Tables/08_ingest.sql"),
    ]);

    expect(sections).toHaveLength(1);
    expect(sections[0]?.count).toBe(3);
    expect(sections[0]?.groups.map((g) => [g.title, g.tables.length])).toEqual([
      ["08_ingest.sql", 1],
      ["13_billing.sql", 2],
    ]);
  });

  // Names are shown as written. An earlier version stripped anything that
  // looked like an ordering prefix, which turned the directory
  // 2026-02-14-index-identifier into "02 14 index identifier" — a rule about
  // someone else's filenames, applied to filenames that did not follow it.
  test("names are passed through verbatim — no prefix stripping, no casing", () => {
    const sections = groupTables([
      table("t1", "sql/db/00_app/02_Tables/01_queue.sql"),
      table("t2", "sql/changes/2026-02-14-index-identifier/001_change.sql"),
    ]);

    expect(sections.map((s) => s.title)).toEqual([
      "sql/changes/2026-02-14-index-identifier",
      "sql/db/00_app/02_Tables",
    ]);
    expect(sections.flatMap((s) => s.groups.map((g) => g.title))).toEqual([
      "001_change.sql",
      "01_queue.sql",
    ]);
  });

  test("directories sort by path so the author's own numeric ordering survives", () => {
    const sections = groupTables([
      table("capture", "sql/db/01_marketing/02_Tables/00_capture.sql"),
      table("invoice", "sql/db/00_app/02_Tables/13_billing.sql"),
    ]);
    expect(sections.map((s) => s.dir)).toEqual([
      "sql/db/00_app/02_Tables",
      "sql/db/01_marketing/02_Tables",
    ]);
  });

  // Anchor ids are what the rail index scrolls to; a collision would send two
  // entries to the same card.
  test("every section and group anchor id is unique", () => {
    const sections = groupTables([
      table("a", "sql/one/x.sql"),
      table("b", "sql/one/y.sql"),
      table("c", "sql/two/x.sql"),
    ]);
    const ids = sections.flatMap((s) => [s.id, ...s.groups.map((g) => g.id)]);
    expect(new Set(ids).size).toBe(ids.length);
  });

  test("a table with no path still groups, under a named placeholder", () => {
    const sections = groupTables([table("loose", "")]);
    expect(sections[0]?.title).toBe("(no directory)");
    expect(sections[0]?.groups[0]?.tables).toHaveLength(1);
  });
});

describe("tableMatches", () => {
  test("matches the table's own name and any column name", () => {
    const t = table("invoice", "sql/x.sql", ["tax_year", "total"]);
    expect(tableMatches(t, "invo")).toBe(true);
    expect(tableMatches(t, "TAX_YEAR")).toBe(true);
    expect(tableMatches(t, "nothing")).toBe(false);
    expect(tableMatches(t, "")).toBe(true);
  });
});
