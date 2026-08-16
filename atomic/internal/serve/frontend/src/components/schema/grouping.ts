// Grouping and filtering for the schema view.
//
// A flat list of a few hundred tables is not navigable. SQL projects already
// carry their own structure in the filesystem — a directory per stage
// (02_tables/, 03_views/) and a file per domain (13_billing.sql,
// 08_ingest.sql) — so the grouping here is the author's, not one imposed by
// this view.
//
// Two levels, because one is not enough: grouping by directory alone put 108
// of one repo's 130 objects in a single section, which is the flat list again
// under a heading. The file is what actually names a domain.
//
// Names are shown as written. An earlier version stripped what looked like
// ordering prefixes (01_queue.sql → "Queue"), which is fine right up until a
// directory is named 2026-02-14-index-identifier and the rule eats the year.
// Any rule here is a rule about someone else's filenames, so there is none.
import type { ApiTableSchema } from "./types";

/** Objects declared in one .sql file. */
export interface SchemaGroup {
  /** Anchor id, targeted by the rail index. */
  id: string;
  /** The file's name, as written. */
  title: string;
  /** Path of the file the objects were declared in. */
  file: string;
  tables: ApiTableSchema[];
}

/** The files in one directory. */
export interface SchemaSection {
  id: string;
  /** The directory, as written. */
  title: string;
  /** Directory the files sit in, "" when unknown. */
  dir: string;
  groups: SchemaGroup[];
  /** Objects across every file in the section. */
  count: number;
}

function dirOf(path: string): string {
  const slash = path.lastIndexOf("/");
  return slash > 0 ? path.slice(0, slash) : "";
}

function baseOf(path: string): string {
  return path.slice(path.lastIndexOf("/") + 1);
}

function slug(prefix: string, path: string): string {
  const body = path.replace(/[^a-zA-Z0-9]+/g, "-").replace(/^-|-$/g, "");
  return `${prefix}-${body || "ungrouped"}`;
}

/** Case-insensitive match on the table's own name or any of its columns —
    "which table has a tax_year column" is as common a question as "where is
    the invoice table". */
export function tableMatches(table: ApiTableSchema, query: string): boolean {
  if (!query) return true;
  const q = query.toLowerCase();
  if (table.node.name.toLowerCase().includes(q)) return true;
  return table.columns.some((c) => c.name.toLowerCase().includes(q));
}

export function groupTables(tables: ApiTableSchema[]): SchemaSection[] {
  const byFile = new Map<string, ApiTableSchema[]>();
  for (const table of tables) {
    const file = table.node.filePath ?? "";
    const bucket = byFile.get(file);
    if (bucket) bucket.push(table);
    else byFile.set(file, [table]);
  }

  const byDir = new Map<string, SchemaGroup[]>();
  for (const [file, group] of [...byFile.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
    const dir = dirOf(file);
    const entry: SchemaGroup = {
      id: slug("schema-file", file),
      title: baseOf(file) || "(no file)",
      file,
      tables: group,
    };
    const bucket = byDir.get(dir);
    if (bucket) bucket.push(entry);
    else byDir.set(dir, [entry]);
  }

  return [...byDir.entries()]
    .sort((a, b) => a[0].localeCompare(b[0]))
    .map(([dir, groups]) => ({
      id: slug("schema", dir),
      title: dir || "(no directory)",
      dir,
      groups,
      count: groups.reduce((n, g) => n + g.tables.length, 0),
    }));
}
