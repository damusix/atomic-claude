// CodeModal — the source + intel two-pane code viewer (Ark Dialog). Mounted
// once by Shell (mirrors components/rail/PageModal). Reuses the carried
// #code-modal / .code-modal-* selectors (public/app.css) the pre-cutover
// htmx dialog styled — only the interaction model changes, same pattern as
// SearchPalette.
//
// Opens via the module-level store (store.ts): openFile (page/rail codeFile
// links), openNode (code-graph.js's window.AtomicCodeExplorer bridge, and
// the search palette's code result). Source pane fetches /api/file/<path>
// (chroma HTML) once per distinct filePath — the effect's dependency array
// is the dedup, mirroring the carried loadSource's currentSourcePath guard.
// Intel pane fetches per back-stack entry's IntelTarget; drill actions push
// a new entry.
import { useEffect, useRef, useState } from "react";
import { useSyncExternalStore } from "react";
import { Dialog } from "@ark-ui/react";
import { attempt } from "@logosdx/utils";
import { api } from "../../utils/api";
import {
  closeModal,
  getState,
  installCodeExplorerGlobal,
  popIntel,
  pushIntel,
  subscribe,
  type StackEntry,
} from "./store";
import {
  intelUrl,
  joinMemberPath,
  type ApiCodeFileResponse,
  type ApiCodeNodeResponse,
  type ApiCodeSubgraphResponse,
  type ApiSourceFileResponse,
  type IntelData,
} from "./types";
import "./style.css";

function SourcePane({ entry }: { entry: StackEntry }) {
  const [source, setSource] = useState<ApiSourceFileResponse | null>(null);
  const [loading, setLoading] = useState(false);
  // Tracked separately from `source === null`: a failed fetch and a fetch that
  // has not started are indistinguishable otherwise, so a 404 rendered as a
  // spinner that never resolved.
  const [failed, setFailed] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);
  const filePath = entry.filePath;

  useEffect(() => {
    if (!filePath) {
      setSource(null);
      setFailed(false);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setFailed(false);
    void attempt(() => api.get<ApiSourceFileResponse>(`/file/${filePath}`)).then(([res, err]) => {
      if (cancelled) return;
      setLoading(false);
      const ok = !err && res?.ok && res.data;
      setSource(ok ? res.data : null);
      setFailed(!ok);
    });
    return () => {
      cancelled = true;
    };
    // filePath is the sole dedup key — a same-file hop (line-only change)
    // does not re-run this effect, matching the carried loadSource guard.
  }, [filePath]);

  useEffect(() => {
    if (!source || !containerRef.current) return;
    const el = entry.line ? containerRef.current.querySelector<HTMLElement>(`#L${entry.line}`) : null;
    if (el) el.scrollIntoView({ block: "center" });
    else containerRef.current.scrollTop = 0;
  }, [source, entry.line]);

  // Not `loading` — that class carries a spinner, and this is a settled
  // answer. A package node has no file to show and never will, so a spinner
  // beside it claims something is still on its way.
  if (!filePath) return <p className="code-source-empty">No source available.</p>;
  if (loading) return <p className="loading">Loading…</p>;

  // The path came from the index, so a miss almost always means the index is
  // describing a file that has since moved or been deleted — say that, rather
  // than leaving a spinner up forever.
  if (failed || !source) {
    return (
      <div className="code-source-missing">
        <p>
          Source not found at <code>{filePath}</code>.
        </p>
        <p className="code-source-missing-hint">
          The code index still lists this path, so it has likely moved or been deleted since
          the last index. Re-run <code>atomic code index</code> in that repository.
        </p>
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      // eslint-disable-next-line react/no-danger -- server-rendered chroma HTML, same trust domain as the page body
      dangerouslySetInnerHTML={{ __html: source.html }}
    />
  );
}

// FileIntel/NodeIntel/SubgraphIntel reuse codeexplorer.go's own class names
// verbatim (renderFileIntel/renderNodeDetail/renderSubgraph) — the carried
// app.css already styles them, so no new CSS is needed here.
function FileIntel({ data, entry }: { data: ApiCodeFileResponse; entry: StackEntry }) {
  if (data.degraded) return <p className="code-file-intel-empty">{data.degraded}</p>;
  const nodes = data.nodes ?? [];
  if (!nodes.length) return <p className="code-file-intel-empty">No symbols in this file.</p>;
  return (
    <div className="code-file-intel">
      <h3 className="code-file-intel-title">Defines ({nodes.length})</h3>
      <ul className="code-file-intel-list">
        {nodes.map((n) => (
          <li key={n.id} className="code-file-intel-chip">
            <button
              type="button"
              className="code-file-intel-name code-node-link"
              onClick={() =>
                pushIntel({
                  filePath: entry.filePath,
                  line: n.startLine,
                  title: n.name,
                  intel: { kind: "node", id: n.id, member: data.member ?? "" },
                })
              }
            >
              {n.name}
            </button>{" "}
            <span className="code-file-intel-kind code-node-kind">{n.kind}</span>
            {n.startLine > 0 ? <span className="code-file-intel-line">:{n.startLine}</span> : null}
          </li>
        ))}
      </ul>
    </div>
  );
}

function NodeIntel({ data, entry }: { data: ApiCodeNodeResponse; entry: StackEntry }) {
  const n = data.node;
  const drill = (mode: "callers" | "callees" | "impact") =>
    pushIntel({
      filePath: entry.filePath,
      line: entry.line,
      title: entry.title,
      intel: { kind: "subgraph", id: n.id, member: data.member, mode },
    });
  return (
    <div className="code-node-detail" data-file={n.filePath} data-line={n.startLine} data-name={n.name}>
      <h2 className="code-node-name">{n.name}</h2>
      <dl className="code-node-meta">
        <dt>Kind</dt>
        <dd className="code-node-kind">{n.kind}</dd>
        {n.filePath ? (
          <>
            <dt>Location</dt>
            <dd>
              {n.filePath}
              {n.startLine ? `:${n.startLine}` : ""}
            </dd>
          </>
        ) : null}
        {n.signature ? (
          <>
            <dt>Signature</dt>
            <dd>
              <code className="code-node-sig">{n.signature}</code>
            </dd>
          </>
        ) : null}
        {n.language ? (
          <>
            <dt>Language</dt>
            <dd>{n.language}</dd>
          </>
        ) : null}
        {n.docstring ? (
          <>
            <dt>Doc</dt>
            <dd>{n.docstring}</dd>
          </>
        ) : null}
      </dl>
      <nav className="code-node-nav">
        <button type="button" onClick={() => drill("callers")}>
          callers
        </button>{" "}
        <button type="button" onClick={() => drill("callees")}>
          callees
        </button>{" "}
        <button type="button" onClick={() => drill("impact")}>
          impact
        </button>
      </nav>
    </div>
  );
}

// Split rather than truncate: see the location markup below for why the
// filename is the half that has to survive.
function dirOf(path: string): string {
  const cut = path.lastIndexOf("/");
  return cut < 0 ? "" : path.slice(0, cut + 1);
}

function baseOf(path: string): string {
  const cut = path.lastIndexOf("/");
  return cut < 0 ? path : path.slice(cut + 1);
}

function SubgraphIntel({ data, entry }: { data: ApiCodeSubgraphResponse; entry: StackEntry }) {
  const others = Object.entries(data.nodes).filter(([id]) => id !== data.root.id);
  if (!others.length) return <p className="code-subgraph-empty">No results.</p>;
  return (
    <div className="code-subgraph">
      <ul className="code-edge-list">
        {others.map(([id, n]) => (
          <li key={id} className="code-edge-chip">
            <button
              type="button"
              className="code-edge-chip-link"
              onClick={() =>
                pushIntel({
                  filePath: n.filePath ? joinMemberPath(data.member, n.filePath) : null,
                  line: n.startLine || null,
                  title: n.name,
                  intel: { kind: "node", id: n.id, member: data.member },
                })
              }
            >
              <span className="code-edge-chip-name">{n.name}</span>
              {n.filePath ? (
                <span className="code-edge-chip-loc" title={n.filePath}>
                  {/* Directory truncates, filename never does. In a 300px
                      pane every one of these paths shares a long prefix, so
                      end-truncating the whole string renders a column of
                      identical-looking rows — the tail is the part that
                      tells them apart. */}
                  <span className="code-edge-chip-dir">{dirOf(n.filePath)}</span>
                  <span className="code-edge-chip-base">
                    {baseOf(n.filePath)}
                    {n.startLine > 0 ? `:${n.startLine}` : ""}
                  </span>
                </span>
              ) : null}
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

function IntelPane({ entry }: { entry: StackEntry }) {
  const [data, setData] = useState<IntelData | null>(null);
  const [loading, setLoading] = useState(false);
  const url = intelUrl(entry.intel);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setData(null);
    const apply = (next: IntelData | null) => {
      if (cancelled) return;
      setLoading(false);
      if (next) setData(next);
    };
    // Branched per intel kind so each fetch is typed to its own response
    // shape and the discriminated IntelData is built literally — a union
    // generic here would force a cast at the setData site.
    const target = entry.intel;
    if (target.kind === "file") {
      void attempt(() => api.get<ApiCodeFileResponse>(url)).then(([res, err]) =>
        apply(err || !res?.ok || !res.data ? null : { kind: "file", ...res.data }),
      );
    } else if (target.kind === "node") {
      void attempt(() => api.get<ApiCodeNodeResponse>(url)).then(([res, err]) =>
        apply(err || !res?.ok || !res.data ? null : { kind: "node", ...res.data }),
      );
    } else {
      void attempt(() => api.get<ApiCodeSubgraphResponse>(url)).then(([res, err]) =>
        apply(err || !res?.ok || !res.data ? null : { kind: "subgraph", ...res.data }),
      );
    }
    return () => {
      cancelled = true;
    };
  }, [url, entry.intel]);

  if (loading || !data) return <p className="loading">Loading…</p>;

  switch (data.kind) {
    case "file":
      return <FileIntel data={data} entry={entry} />;
    case "node":
      return <NodeIntel data={data} entry={entry} />;
    case "subgraph":
      return <SubgraphIntel data={data} entry={entry} />;
  }
}

export function CodeModal() {
  const state = useSyncExternalStore(subscribe, getState, getState);
  const top = state.stack[state.stack.length - 1];

  useEffect(() => {
    installCodeExplorerGlobal();
  }, []);

  return (
    <Dialog.Root
      open={state.open}
      onOpenChange={(details) => {
        if (!details.open) closeModal();
      }}
      lazyMount
      unmountOnExit
      aria-label="Source file viewer"
    >
      {/* app.css shows #code-modal only with .open — the class must track
          state, not be hardcoded, or the scrim renders permanently (Ark keeps
          content in the DOM when neither lazyMount nor unmountOnExit strip it,
          and .code-modal-box's display:flex overrides the hidden attr). */}
      <Dialog.Positioner id="code-modal" className={state.open ? "open" : ""}>
        <Dialog.Content className="code-modal-box">
          <div className="code-modal-header">
            <Dialog.Title className="code-modal-title">{top?.title ?? ""}</Dialog.Title>
            <Dialog.CloseTrigger className="code-modal-close" aria-label="Close">
              ✕
            </Dialog.CloseTrigger>
          </div>
          <div className="code-modal-body">
            <div id="code-modal-source">{top ? <SourcePane entry={top} /> : null}</div>
            <div id="code-modal-intel-pane">
              <button
                type="button"
                className="code-modal-intel-back"
                hidden={state.stack.length <= 1}
                onClick={popIntel}
              >
                ← Back
              </button>
              <div id="code-modal-intel">{top ? <IntelPane key={intelUrl(top.intel)} entry={top} /> : null}</div>
            </div>
          </div>
        </Dialog.Content>
      </Dialog.Positioner>
    </Dialog.Root>
  );
}
