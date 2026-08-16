import { describe, expect, test } from "bun:test";
import { columnViews, constraintLabel, keyViews } from "./columns";
import type { ApiCodeNode } from "../code-modal/types";

function col(name: string, dataType?: string, line = 1): ApiCodeNode {
  return { id: `c:${name}:${line}`, name, kind: "column", filePath: "x.sql", startLine: line, dataType };
}

function constraint(name: string, constraintType: string, constraintColumns: string[] = []): ApiCodeNode {
  return {
    id: `k:${name}`,
    name,
    kind: "constraint",
    filePath: "x.sql",
    startLine: 1,
    constraintType,
    constraintColumns,
  };
}

describe("columnViews", () => {
  // A re-runnable schema script declares a column in CREATE TABLE and again in
  // a defensive ALTER TABLE … ADD COLUMN IF NOT EXISTS. Both are real
  // declaration sites, so the graph holds both — but the card must show the
  // column once, and show the declaration that actually states a type.
  test("collapses a column declared twice, keeping the typed declaration", () => {
    const views = columnViews([col("updated_at", "TG_TS", 25), col("updated_at", undefined, 39)]);
    expect(views).toHaveLength(1);
    expect(views[0]?.dataType).toBe("TG_TS");
  });

  test("keeps the typed one regardless of which came first", () => {
    const views = columnViews([col("value", undefined, 50), col("value", "JSONB", 21)]);
    expect(views).toHaveLength(1);
    expect(views[0]?.dataType).toBe("JSONB");
  });

  test("a column with no type anywhere still renders", () => {
    const views = columnViews([col("mystery")]);
    expect(views).toHaveLength(1);
    expect(views[0]?.dataType).toBe("");
  });

  // Key membership is read from the constraint's declared column list.
  test("marks key columns from the constraint's own column list", () => {
    const views = columnViews([
      col("party_no", "TG_NO"),
      col("email", "TG_EMAIL"),
      col("note", "TEXT"),
      constraint("pk_app_setting", "primary_key", ["party_no"]),
      constraint("fk_app_setting_1", "foreign_key", ["party_no"]),
      constraint("uq_app_setting__email", "unique", ["email"]),
    ]);
    const by = Object.fromEntries(views.map((v) => [v.name, v]));
    expect(by.party_no?.pk).toBe(true);
    expect(by.party_no?.fk).toBe(true);
    expect(by.email?.unique).toBe(true);
    expect(by.email?.pk).toBe(false);
    expect(by.note?.pk).toBe(false);
    expect(by.note?.fk).toBe(false);
    expect(by.note?.unique).toBe(false);
  });

  test("every column of a composite key is marked", () => {
    const views = columnViews([
      col("party_no"),
      col("sales_order_no"),
      col("item_no"),
      constraint("pk_sales_order_item", "primary_key", ["party_no", "sales_order_no"]),
    ]);
    const by = Object.fromEntries(views.map((v) => [v.name, v]));
    expect([by.party_no?.pk, by.sales_order_no?.pk, by.item_no?.pk]).toEqual([true, true, false]);
  });

  // The old behaviour: pk_queue_status contains "status", so a status column
  // was marked by coincidence. The declared list says otherwise.
  test("a constraint named after its table marks nothing it does not declare", () => {
    const views = columnViews([
      col("status", "TG_CODE"),
      constraint("pk_queue_status", "primary_key", ["queue_no"]),
    ]);
    expect(views[0]?.pk).toBe(false);
  });

  // A key whose columns the extractor could not read marks nothing, rather
  // than falling back to reading its name.
  test("a constraint with no declared columns marks nothing", () => {
    const views = columnViews([col("param"), constraint("pk_app_setting_param", "primary_key", [])]);
    expect(views[0]?.pk).toBe(false);
  });
});

describe("keyViews", () => {
  test("indexes and constraints both list, each carrying its kind and columns", () => {
    const views = keyViews([
      col("param"),
      constraint("pk_app_setting", "primary_key", ["param"]),
      { id: "i:1", name: "ix_claim", kind: "index", filePath: "x.sql", startLine: 1 },
    ]);
    expect(views.map((v) => [v.name, v.kind, v.columns])).toEqual([
      ["pk_app_setting", "primary_key", ["param"]],
      ["ix_claim", "index", []],
    ]);
  });

  // Same duplication the columns have: declared in CREATE TABLE, added again
  // defensively by an ALTER.
  test("collapses a constraint declared twice, keeping the one naming its columns", () => {
    const views = keyViews([
      constraint("ck_kind", "check", []),
      constraint("pk_app_setting", "primary_key", []),
      constraint("pk_app_setting", "primary_key", ["param"]),
    ]);
    expect(views).toHaveLength(2);
    expect(views.find((v) => v.name === "pk_app_setting")?.columns).toEqual(["param"]);
  });

  test("a constraint with no recorded kind falls back to check, not blank", () => {
    const views = keyViews([{ id: "k:x", name: "ck_x", kind: "constraint", filePath: "x.sql", startLine: 1 }]);
    expect(views[0]?.kind).toBe("check");
  });
});

describe("constraintLabel", () => {
  test("abbreviates the kinds a schema reader scans for", () => {
    expect(constraintLabel("primary_key")).toBe("PK");
    expect(constraintLabel("foreign_key")).toBe("FK");
    expect(constraintLabel("index")).toBe("INDEX");
  });

  test("an unrecognised kind is shown, not swallowed", () => {
    expect(constraintLabel("exclusion_thing")).toBe("EXCLUSION THING");
  });
});
