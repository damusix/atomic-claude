// External — /external page: the external-link registry. Fetches
// /api/external (external.go: apiExternalResponse) and renders a sorted
// table (URL · source pages · first seen), mirroring external.go's carried
// externalBodyTmplStr fragment.
import { Link } from "react-router";
import { useApi } from "../utils/api";

interface ApiExternalEntry {
  url: string;
  sources: string[];
  firstSeen: string | null;
}

interface ApiExternalResponse {
  entries: ApiExternalEntry[];
}

export function External() {
  const { data, loading, failure } = useApi().get<ApiExternalResponse>("/external");

  return (
    <div className="md-content" data-route="external">
      <h1>External links registry</h1>
      <p>All outbound http(s) URLs across the realm, with source pages and first-seen date.</p>

      {loading && !data ? <p className="loading">Loading…</p> : null}
      {failure ? <p>Could not load the external-link registry.</p> : null}

      {data && data.entries.length === 0 ? <p>No external links found in this realm.</p> : null}

      {data && data.entries.length > 0 ? (
        <table className="external-registry">
          <thead>
            <tr>
              <th>URL</th>
              <th>Source pages</th>
              <th>First seen</th>
            </tr>
          </thead>
          <tbody>
            {data.entries.map((entry) => (
              <tr key={entry.url}>
                <td>
                  <a href={entry.url} target="_blank" rel="noopener noreferrer">
                    {entry.url}
                  </a>
                </td>
                <td>
                  {entry.sources.map((src, i) => (
                    <span key={src}>
                      {i > 0 ? " " : ""}
                      <Link to={`/page/${src}`} className="nav-item">
                        {src}
                      </Link>
                    </span>
                  ))}
                </td>
                <td>{entry.firstSeen ?? "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      ) : null}
    </div>
  );
}
