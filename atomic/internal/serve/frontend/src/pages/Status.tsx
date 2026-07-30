// Status — /status page: wiki staleness + code-index health dashboard.
// Fetches /api/status (health.go: apiStatusResponse) and mirrors
// health.go's carried healthTmpl fragment (badge-per-item lists, severity
// badge, all-fresh summary).
import { useApi } from "../utils/api";

interface ApiWikiStatus {
  staleRepos: string[];
  staleConcerns: string[];
  staleBuckets: string[];
  bucketDiffKeys: string[];
  allFresh: boolean;
}

interface ApiIndexStatus {
  severity: string;
  detail: string;
  freshCount: number;
  staleMembers: string[];
  notIndexed: string[];
}

interface ApiStatusResponse {
  isRealmScope: boolean;
  wiki: ApiWikiStatus;
  index: ApiIndexStatus;
}

export function Status() {
  const { data, loading, failure } = useApi().get<ApiStatusResponse>("/status");

  if (loading && !data) {
    return (
      <div className="page-content-inner health-dashboard" data-route="status">
        <p className="loading">Loading…</p>
      </div>
    );
  }
  if (failure || !data) {
    return (
      <div className="page-content-inner health-dashboard" data-route="status">
        <p>Could not load realm health.</p>
      </div>
    );
  }

  const allFresh = data.isRealmScope && data.wiki.allFresh && data.index.severity === "PASS";

  return (
    <div className="page-content-inner health-dashboard" data-route="status">
      <h2 className="health-title">Realm Health</h2>

      {data.isRealmScope ? (
        <section className="health-section">
          <h3>Wiki Staleness</h3>
          {data.wiki.allFresh ? (
            <p className="health-ok">All wiki artifacts are fresh.</p>
          ) : (
            <ul className="health-list">
              {data.wiki.staleRepos.map((name) => (
                <li key={`repo:${name}`}>
                  <span className="badge badge-stale">stale repo</span> {name}
                </li>
              ))}
              {data.wiki.staleConcerns.map((name) => (
                <li key={`concern:${name}`}>
                  <span className="badge badge-stale">stale concern</span> {name}
                </li>
              ))}
              {data.wiki.bucketDiffKeys.map((name) => (
                <li key={`bucket:${name}`}>
                  <span className="badge badge-diff">bucket diff</span> {name}
                </li>
              ))}
            </ul>
          )}
        </section>
      ) : (
        <p className="health-info">No realm wiki — showing repo code-index health only.</p>
      )}

      <section className="health-section">
        <h3>Code Index</h3>
        <p className={data.index.severity === "PASS" ? "health-detail health-ok" : "health-detail health-warn"}>
          <span className={`badge badge-severity-${data.index.severity}`}>{data.index.severity}</span>{" "}
          {data.index.detail}
        </p>
        {data.index.staleMembers.length > 0 ? (
          <ul className="health-list">
            {data.index.staleMembers.map((name) => (
              <li key={`stale:${name}`}>
                <span className="badge badge-stale">stale index</span> {name}
              </li>
            ))}
          </ul>
        ) : null}
        {data.index.notIndexed.length > 0 ? (
          <ul className="health-list">
            {data.index.notIndexed.map((name) => (
              <li key={`missing:${name}`}>
                <span className="badge badge-missing">not indexed</span> {name}
              </li>
            ))}
          </ul>
        ) : null}
      </section>

      {allFresh ? <p className="health-ok health-all-fresh">All fresh — realm is healthy.</p> : null}
    </div>
  );
}
