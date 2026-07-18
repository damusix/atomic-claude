// External — /external page: the external-link registry. Fetches
// /api/external (external.go: apiExternalResponse, memoized server-side by
// realm fingerprint) and renders the entries grouped by domain, each group a
// collapsed count header over its URL rows (URL · source pages · first seen).
import { useMemo } from "react";
import { Link } from "react-router";
import { attemptSync } from "@logosdx/utils";
import { useApi } from "../utils/api";

interface ApiExternalEntry {
  url: string;
  sources: string[];
  firstSeen: string | null;
}

interface ApiExternalResponse {
  entries: ApiExternalEntry[];
}

function hostOf(url: string): string {
  const [host] = attemptSync(() => new URL(url).host);
  return host ?? "(invalid url)";
}

function groupByDomain(entries: ApiExternalEntry[]): [string, ApiExternalEntry[]][] {
  const groups = new Map<string, ApiExternalEntry[]>();
  for (const entry of entries) {
    const host = hostOf(entry.url);
    const bucket = groups.get(host);
    if (bucket) bucket.push(entry);
    else groups.set(host, [entry]);
  }
  return [...groups.entries()].sort((a, b) => b[1].length - a[1].length || a[0].localeCompare(b[0]));
}

function DomainGroup({ host, entries }: { host: string; entries: ApiExternalEntry[] }) {
  return (
    <details className="external-domain" open={false}>
      <summary className="external-domain-header">
        {host} <span className="external-domain-count">{entries.length}</span>
      </summary>
      <table className="external-registry">
        <tbody>
          {entries.map((entry) => (
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
              <td className="external-date">{entry.firstSeen ?? "—"}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </details>
  );
}

export function External() {
  const { data, loading, failure } = useApi().get<ApiExternalResponse>("/external");
  const groups = useMemo(() => groupByDomain(data?.entries ?? []), [data]);

  return (
    <div className="page-content-inner md-content" data-route="external">
      <h1>External links registry</h1>
      <p>All outbound http(s) URLs across the realm, grouped by domain.</p>

      {loading && !data ? <p className="loading">Loading…</p> : null}
      {failure ? <p>Could not load the external-link registry.</p> : null}

      {data && groups.length === 0 ? <p>No external links found in this realm.</p> : null}

      {groups.map(([host, entries]) => (
        <DomainGroup key={host} host={host} entries={entries} />
      ))}
    </div>
  );
}
