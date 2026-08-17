// Schema shapes — mirrors the /api/code/schema response
// (atomic/internal/serve/codeexplorer.go: apiCodeSchemaResponse / apiTableSchema).
import type { ApiCodeNode } from "../code-modal/types";

export interface ApiTableSchema {
  node: ApiCodeNode;
  columns: ApiCodeNode[];
  fkSources: ApiCodeNode[];
  writers: ApiCodeNode[];
}

/** A stored routine and the tables it touches. */
export interface ApiRoutineSchema {
  node: ApiCodeNode;
  reads: ApiCodeNode[];
  writes: ApiCodeNode[];
}

export interface ApiCodeSchemaResponse {
  tables: ApiTableSchema[];
  /** Stored procedures and SQL functions. */
  routines: ApiRoutineSchema[];
  /** User-defined SQL types — Postgres domains and composite types. */
  types: ApiCodeNode[];
  // Soft state: set (with tables empty) when no index is available for the
  // requested member — mirrors ApiCodeFileResponse.degraded.
  degraded?: string;
}

/** Whether the shell should offer the schema view at all. */
export interface ApiCapabilitiesResponse {
  schema: boolean;
  /** "config" when .claude/atomic.toml decided, "detected" otherwise. */
  source: string;
}
