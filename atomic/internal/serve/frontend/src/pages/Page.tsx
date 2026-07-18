// Route stub for /page/<relpath> and "/" (landing). Real content injection
// (HTML-in-JSON, wikilink interception, mermaid mount) lands in CP6.
import { useParams } from "react-router";

export function Page() {
  const params = useParams();
  const relpath = params["*"] ?? "";
  return <div data-route="page">Page: {relpath || "(landing)"}</div>;
}
