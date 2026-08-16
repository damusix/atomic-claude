// Column presentation for a table card.
//
// The card used to render a bare list of names — nothing said they were
// columns rather than, say, files. The index carries each column's declared
// type and each constraint's declared column list; both are read, neither is
// inferred. An earlier version guessed key membership from constraint names,
// which a repo naming its keys `pk_<table>` defeats entirely and a table named
// after one of its own columns defeats in the other direction.
import type { ApiCodeNode } from "../code-modal/types";

/** A column as the card draws it: name, declared type, and its role in the
    table's keys. */
export interface ColumnView {
  node: ApiCodeNode;
  name: string;
  /** Declared type, "" when the extractor could not read one. */
  dataType: string;
  /** Part of the primary key. */
  pk: boolean;
  /** Part of a foreign key. */
  fk: boolean;
  /** Covered by a unique constraint. */
  unique: boolean;
}

/** A constraint or index as the card draws it. */
export interface KeyView {
  node: ApiCodeNode;
  name: string;
  /** primary_key | foreign_key | unique | check | index */
  kind: string;
  /** Columns it covers, as declared; empty when the declaration names none
      (a CHECK constrains an expression, not a column list). */
  columns: string[];
}

const CONSTRAINT_LABEL: Record<string, string> = {
  primary_key: "PK",
  foreign_key: "FK",
  unique: "UNIQUE",
  check: "CHECK",
  index: "INDEX",
};

export function constraintLabel(kind: string): string {
  return CONSTRAINT_LABEL[kind] ?? kind.replace(/_/g, " ").toUpperCase();
}

/**
 * Builds the card's column rows.
 *
 * Duplicates are collapsed by name: a re-runnable schema script declares a
 * column in CREATE TABLE and again in a defensive `ALTER TABLE … ADD COLUMN IF
 * NOT EXISTS`, so the graph legitimately holds two declaration sites for one
 * column. The graph is right; listing the column twice is not. The declaration
 * carrying a type wins, since that is the one that says anything.
 */
export function columnViews(nodes: ApiCodeNode[]): ColumnView[] {
  const columns = nodes.filter((n) => n.kind === "column");
  const constraints = nodes.filter((n) => n.kind === "constraint");

  const byName = new Map<string, ApiCodeNode>();
  for (const col of columns) {
    const key = col.name.toLowerCase();
    const existing = byName.get(key);
    if (!existing || (!existing.dataType && col.dataType)) byName.set(key, col);
  }

  // One pass over the constraints builds the membership sets, so each column
  // is a lookup rather than a scan.
  const inKey = { primary_key: new Set<string>(), foreign_key: new Set<string>(), unique: new Set<string>() };
  for (const c of constraints) {
    const set = inKey[c.constraintType as keyof typeof inKey];
    if (!set) continue;
    for (const name of c.constraintColumns ?? []) set.add(name.toLowerCase());
  }

  return [...byName.values()].map((node) => {
    const key = node.name.toLowerCase();
    return {
      node,
      name: node.name,
      dataType: node.dataType ?? "",
      pk: inKey.primary_key.has(key),
      fk: inKey.foreign_key.has(key),
      unique: inKey.unique.has(key),
    };
  });
}

/** Keys and indexes, one entry per name.
 *
 * Same duplication as the columns: a re-runnable script declares a constraint
 * in CREATE TABLE and adds it again defensively, so the graph holds two
 * declaration sites. The declaration that names its columns wins. */
export function keyViews(nodes: ApiCodeNode[]): KeyView[] {
  const byName = new Map<string, KeyView>();
  for (const node of nodes) {
    if (node.kind !== "constraint" && node.kind !== "index") continue;
    const view: KeyView = {
      node,
      name: node.name,
      kind: node.kind === "index" ? "index" : (node.constraintType ?? "check"),
      columns: node.constraintColumns ?? [],
    };
    const key = node.name.toLowerCase();
    const existing = byName.get(key);
    if (!existing || (existing.columns.length === 0 && view.columns.length > 0)) {
      byName.set(key, view);
    }
  }
  return [...byName.values()];
}
