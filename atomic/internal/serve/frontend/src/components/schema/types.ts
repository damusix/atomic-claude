// Schema shapes — mirrors the /api/code/schema response
// (atomic/internal/serve/codeexplorer.go: apiCodeSchemaResponse / apiTableSchema).
import type { ApiCodeNode } from "../code-modal/types";

export interface ApiTableSchema {
  node: ApiCodeNode;
  columns: ApiCodeNode[];
  fkSources: ApiCodeNode[];
  writers: ApiCodeNode[];
}

export interface ApiCodeSchemaResponse {
  tables: ApiTableSchema[];
  // Soft state: set (with tables empty) when no index is available for the
  // requested member — mirrors ApiCodeFileResponse.degraded.
  degraded?: string;
}
