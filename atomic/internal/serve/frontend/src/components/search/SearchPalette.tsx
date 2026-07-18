// SearchPalette — the ⌘K command palette (Ark Combobox). Reuses the carried
// #search-modal / .search-modal-* / .search-toggle / .search-results
// selectors (public/app.css) the pre-cutover htmx dialog styled, so no new
// visual work lands here — only the interaction model changes.
//
// Shortcuts: ⌘K / Ctrl+K opens; "/" opens when focus isn't in a text field;
// Escape closes. Debounced (200ms, matches templates/layout.html's dialog)
// fetch to /api/search/md or /api/code/search depending on the md|code
// toggle. Selecting a markdown result navigates via React Router. Selecting
// a code result opens the code modal (components/code-modal) via its
// openNode seam — the search result already carries name/file/line, so the
// modal's source pane can open without a second lookup.
import { useEffect, useMemo, useRef, useState } from "react";
import { Combobox, createListCollection } from "@ark-ui/react";
import { attempt } from "@logosdx/utils";
import { useNavigate } from "react-router";
import { openNode } from "../code-modal/store";
import { api } from "../../utils/api";
import { codePaletteItems, mdPaletteItems, type PaletteItem } from "./searchItems";
import type { ApiCodeSearchResponse, ApiMdSearchResponse } from "./types";
import "./style.css";

const DEBOUNCE_MS = 200;

export function SearchPalette({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const navigate = useNavigate();
  const [source, setSource] = useState<"md" | "code">("md");
  const [query, setQuery] = useState("");
  const [debounced, setDebounced] = useState("");
  const [loading, setLoading] = useState(false);
  const [mdResults, setMdResults] = useState<ApiMdSearchResponse | null>(null);
  const [codeResults, setCodeResults] = useState<ApiCodeSearchResponse | null>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Global shortcuts — mounted regardless of `open` so ⌘K/"/" work from
  // anywhere, mirroring the pre-cutover dialog's document-level listener.
  useEffect(() => {
    function handler(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && (e.key === "k" || e.key === "K")) {
        e.preventDefault();
        onOpenChange(true);
        return;
      }
      if (e.key === "Escape" && open) {
        onOpenChange(false);
        return;
      }
      if (e.key === "/" && !open) {
        const target = e.target as HTMLElement | null;
        const tag = target?.tagName;
        if (tag !== "INPUT" && tag !== "TEXTAREA" && !target?.isContentEditable) {
          e.preventDefault();
          onOpenChange(true);
        }
      }
    }
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [open, onOpenChange]);

  useEffect(() => {
    if (open) inputRef.current?.focus();
  }, [open]);

  // Debounce the raw input before it drives a fetch.
  useEffect(() => {
    const t = setTimeout(() => setDebounced(query.trim()), DEBOUNCE_MS);
    return () => clearTimeout(t);
  }, [query]);

  useEffect(() => {
    let cancelled = false;
    if (!debounced) {
      setMdResults(null);
      setCodeResults(null);
      setLoading(false);
      return;
    }
    setLoading(true);
    const path =
      source === "md"
        ? `/search/md?q=${encodeURIComponent(debounced)}`
        : `/code/search?q=${encodeURIComponent(debounced)}`;
    void attempt(() => api.get<ApiMdSearchResponse | ApiCodeSearchResponse>(path)).then(([res, err]) => {
      if (cancelled) return;
      setLoading(false);
      if (err || !res?.ok) {
        setMdResults(null);
        setCodeResults(null);
        return;
      }
      if (source === "md") setMdResults(res.data as ApiMdSearchResponse);
      else setCodeResults(res.data as ApiCodeSearchResponse);
    });
    return () => {
      cancelled = true;
    };
  }, [debounced, source]);

  const items = useMemo<PaletteItem[]>(
    () => (source === "md" ? mdPaletteItems(mdResults) : codePaletteItems(codeResults)),
    [source, mdResults, codeResults],
  );

  const collection = useMemo(
    () => createListCollection({ items, itemToValue: (i: PaletteItem) => i.id, itemToString: (i: PaletteItem) => i.label }),
    [items],
  );

  function close() {
    onOpenChange(false);
  }

  function handleSelect(details: { value: string[] }) {
    const item = items.find((i) => i.id === details.value[0]);
    if (!item) return;
    if (item.kind === "md" && item.relpath) {
      close();
      navigate(`/page/${item.relpath}`);
      return;
    }
    if (item.kind === "code" && item.codeId && item.member !== undefined) {
      close();
      openNode(item.codeId, item.member, { title: item.label, file: item.filePath, line: item.startLine });
      return;
    }
    close();
  }

  function gotoSearchPage() {
    const q = query.trim();
    if (!q) return;
    close();
    navigate(`/search?q=${encodeURIComponent(q)}&src=${source}`);
  }

  if (!open) return <div id="search-modal" role="dialog" aria-modal="true" aria-label="Search dialog" />;

  return (
    <div
      id="search-modal"
      className="open"
      role="dialog"
      aria-modal="true"
      aria-label="Search dialog"
      onClick={close}
    >
      <div className="search-modal-box" onClick={(e) => e.stopPropagation()}>
        <Combobox.Root
          collection={collection}
          inputValue={query}
          onInputValueChange={(d) => setQuery(d.inputValue)}
          onValueChange={handleSelect}
          onKeyDownCapture={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              gotoSearchPage();
            }
          }}
        >
          <div className="search-modal-header">
            <span className="search-modal-icon" aria-hidden="true">
              ⌕
            </span>
            <Combobox.Control style={{ flex: 1, display: "flex" }}>
              <Combobox.Input
                ref={inputRef}
                className="search-modal-input"
                placeholder="Search…"
                aria-label="Search"
              />
            </Combobox.Control>
            <span className="search-toggle" role="group" aria-label="Search source">
              <button
                type="button"
                className={`toggle-btn${source === "md" ? " toggle-active" : ""}`}
                aria-pressed={source === "md"}
                onClick={() => setSource("md")}
              >
                md
              </button>
              <button
                type="button"
                className={`toggle-btn${source === "code" ? " toggle-active" : ""}`}
                aria-pressed={source === "code"}
                onClick={() => setSource("code")}
              >
                code
              </button>
            </span>
          </div>
          <Combobox.Positioner>
            <Combobox.Content className="search-results" aria-label="Search results">
              <SearchResultsBody
                source={source}
                debounced={debounced}
                loading={loading}
                items={items}
                codeResults={codeResults}
              />
            </Combobox.Content>
          </Combobox.Positioner>
        </Combobox.Root>
        <div className="search-modal-footer">
          <button type="button" className="search-viewall" onClick={gotoSearchPage}>
            View all results →
          </button>
          <span className="search-modal-hint">
            <kbd>Enter</kbd> view all · <kbd>Esc</kbd> close
          </span>
        </div>
      </div>
    </div>
  );
}

function SearchResultsBody({
  source,
  debounced,
  loading,
  items,
  codeResults,
}: {
  source: "md" | "code";
  debounced: string;
  loading: boolean;
  items: PaletteItem[];
  codeResults: ApiCodeSearchResponse | null;
}) {
  if (!debounced) return <p className="md-search-empty">Type to search…</p>;
  if (loading) return <p className="loading search-loading">Searching…</p>;

  if (source === "md") {
    if (!items.length) return <p className="md-search-empty">No results.</p>;
    return (
      <ul className="md-search-result-list">
        {items.map((item) => (
          <Combobox.Item key={item.id} item={item} className="md-search-result">
            <Combobox.ItemText>
              <span className="md-search-link">
                <span className="md-search-loc">{item.label}</span>
                <span className="md-search-snippet">{item.sub}</span>
              </span>
            </Combobox.ItemText>
          </Combobox.Item>
        ))}
      </ul>
    );
  }

  const members = codeResults?.members ?? [];
  if (!members.length) return <p className="code-search-empty">No results.</p>;

  return (
    <>
      {members.map((m) => (
        <div key={m.key}>
          <p className="code-search-group-header">{m.prefix}</p>
          {!m.indexed ? (
            <p className="code-search-not-indexed">
              not indexed — run <code>atomic code index</code>
            </p>
          ) : m.results.length === 0 ? (
            <p className="code-search-empty">No results.</p>
          ) : (
            <ul className="code-search-result-list">
              {m.results.map((n) => {
                const item = items.find((i) => i.id === `code:${m.key}:${n.id}`);
                if (!item) return null;
                return (
                  <Combobox.Item key={item.id} item={item} className="code-search-result">
                    <Combobox.ItemText>
                      <span className="code-search-link">
                        <span className="code-search-name">{n.name}</span>{" "}
                        <span className="code-search-kind">{n.filePath}:{n.startLine}</span>
                      </span>
                    </Combobox.ItemText>
                  </Combobox.Item>
                );
              })}
            </ul>
          )}
        </div>
      ))}
    </>
  );
}
