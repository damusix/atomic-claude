// Search — /search?q=&src= page. Opens one SSE connection to
// /api/search/stream (src=all always — the src param only picks which tab
// is active; "all" already carries both md and code results, so switching
// tabs re-renders from the same stream instead of re-fetching) and streams
// results into the All|Markdown|Code tabs (Ark Tabs) as they arrive,
// spinner until the terminal "end" event. Code results are text-only here
// (no code modal until CP9) — same seam as SearchPalette's code selection.
import { useState } from "react";
import { Tabs } from "@ark-ui/react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { useSearchStream } from "../components/search/useSearchStream";
import { normalizeSearchSrc, type SearchSrc } from "../components/search/types";

export function Search() {
  const [params, setParams] = useSearchParams();
  const query = params.get("q")?.trim() ?? "";
  const src = normalizeSearchSrc(params.get("src"));
  const stream = useSearchStream(query, "all");

  function handleTabChange(details: { value: string }) {
    const next = normalizeSearchSrc(details.value);
    setParams(
      (prev) => {
        const p = new URLSearchParams(prev);
        p.set("src", next);
        return p;
      },
      { replace: true },
    );
  }

  return (
    <div className="page-content-inner md-content search-page" data-route="search">
      <h1 className="search-page-title">Search</h1>
      {query ? (
        <>
          <p className="search-page-query">
            Results for <strong>{query}</strong>
          </p>
          <Tabs.Root value={src} onValueChange={handleTabChange}>
            <Tabs.List className="search-page-tabs">
              <Tabs.Trigger value="all" className="search-tab">
                All
              </Tabs.Trigger>
              <Tabs.Trigger value="md" className="search-tab">
                Markdown
              </Tabs.Trigger>
              <Tabs.Trigger value="code" className="search-tab">
                Code
              </Tabs.Trigger>
            </Tabs.List>
            {/* Ark keeps every Tabs.Content mounted (hidden, not removed) so
                switching tabs never re-fetches — a single content element
                keyed on the active tab avoids tripling the result markup
                that a Content-per-value would otherwise leave in the DOM. */}
            <Tabs.Content value={src}>
              <SearchSections active={src} stream={stream} />
            </Tabs.Content>
          </Tabs.Root>
        </>
      ) : (
        <SearchForm />
      )}
    </div>
  );
}

function SearchSections({ active, stream }: { active: SearchSrc; stream: ReturnType<typeof useSearchStream> }) {
  return (
    <>
      {(active === "all" || active === "md") && (
        <section className="search-page-section">
          <h2 className="search-page-section-title">Markdown</h2>
          <div className="search-page-results" data-section="md">
            {!stream.md ? (
              <p className="loading search-loading">Searching markdown…</p>
            ) : stream.md.results.length === 0 ? (
              <p className="md-search-empty">No results.</p>
            ) : (
              <ul className="md-search-result-list">
                {stream.md.results.map((r) => (
                  <li key={`${r.relpath}:${r.line}`} className="md-search-result">
                    <Link to={`/page/${r.relpath}`} className="md-search-link">
                      <span className="md-search-loc">
                        {r.relpath}:{r.line}
                      </span>
                      <span className="md-search-snippet">{r.snippet}</span>
                    </Link>
                  </li>
                ))}
              </ul>
            )}
          </div>
        </section>
      )}
      {(active === "all" || active === "code") && (
        <section className="search-page-section">
          <h2 className="search-page-section-title">Code</h2>
          <div className="search-page-results" data-section="code">
            {!stream.done ? (
              <p className="loading search-loading">Searching code…</p>
            ) : stream.code.length === 0 ? (
              <p className="code-search-empty">No results.</p>
            ) : (
              stream.code.map((group) => (
                <div key={group.member.key}>
                  <p className="code-search-group-header">{group.member.prefix}</p>
                  {!group.member.indexed ? (
                    <p className="code-search-not-indexed">
                      not indexed — run <code>atomic code index</code>
                    </p>
                  ) : group.results.length === 0 ? (
                    <p className="code-search-empty">No results.</p>
                  ) : (
                    <ul className="code-search-result-list">
                      {group.results.map((n) => (
                        <li key={n.id} className="code-search-result">
                          <span className="code-search-link">
                            <span className="code-search-name">{n.name}</span>{" "}
                            <span className="code-search-kind">
                              {n.filePath}:{n.startLine}
                            </span>
                          </span>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              ))
            )}
          </div>
        </section>
      )}
    </>
  );
}

function SearchForm() {
  const [value, setValue] = useState("");
  const navigate = useNavigate();

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const q = value.trim();
    if (!q) return;
    navigate(`/search?q=${encodeURIComponent(q)}&src=all`);
  }

  return (
    <>
      <p className="search-page-hint">
        Open the search dialog with <kbd>⌘K</kbd> / <kbd>Ctrl K</kbd>, or search here:
      </p>
      <form className="search-page-form" onSubmit={handleSubmit}>
        <input
          className="search-page-input"
          value={value}
          onChange={(e) => setValue(e.target.value)}
          type="search"
          placeholder="Search markdown or code…"
          autoComplete="off"
          autoFocus
        />
        <button type="submit">Search</button>
      </form>
    </>
  );
}
