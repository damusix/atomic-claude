// Route for /code/schema — the SPA home for the SQL schema view (preserves
// the pre-cutover URL). Thin wrapper: all behavior lives in
// components/schema/SchemaView.
import { SchemaView } from "../components/schema/SchemaView";

export function Schema() {
  return <SchemaView />;
}
