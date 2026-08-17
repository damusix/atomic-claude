// graphWarm — lay the docs graph out in the background so opening the graph
// is instant instead of a ~15s wait.
//
// The cost is the force simulation, not the fetch (/graph/data returns in
// ~130ms). The engine already caches a settled layout in IndexedDB keyed by a
// content fingerprint and replays a hit with zero motion, so warming means
// getting that cache populated before the reader asks for it.
//
// NOT a Web Worker, deliberately. cosmos.gl is WebGL over a DOM canvas and
// reads `window`; it cannot run in a worker without OffscreenCanvas support
// it does not have. The warm therefore runs on the main thread in an
// offscreen container — visually hidden but laid out, because a display:none
// container has no size and WebGL renders nothing into it.
import { mountGraph, teardownGraph } from "./graphEngineAdapter";

const DB_NAME = "atomic-graph-warm";
const STORE = "runs";
const RECORD_KEY = "docs";

/** How long a "building" record is trusted before another tab may take over.
    A tab that is closed mid-warm never clears its record, so without a
    takeover window the warm would be blocked until the browser data is. */
const STALE_BUILD_MS = 3 * 60 * 1000;

interface WarmRecord {
  runId: string;
  state: "building" | "done";
  at: number;
}

function openDB(): Promise<IDBDatabase | null> {
  return new Promise((resolve) => {
    if (typeof indexedDB === "undefined") return resolve(null);
    const req = indexedDB.open(DB_NAME, 1);
    req.onupgradeneeded = () => {
      if (!req.result.objectStoreNames.contains(STORE)) req.result.createObjectStore(STORE);
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => resolve(null);
  });
}

function readRecord(db: IDBDatabase): Promise<WarmRecord | null> {
  return new Promise((resolve) => {
    const req = db.transaction(STORE, "readonly").objectStore(STORE).get(RECORD_KEY);
    req.onsuccess = () => resolve((req.result as WarmRecord) ?? null);
    req.onerror = () => resolve(null);
  });
}

function writeRecord(db: IDBDatabase, record: WarmRecord): Promise<void> {
  return new Promise((resolve) => {
    const tx = db.transaction(STORE, "readwrite");
    tx.objectStore(STORE).put(record, RECORD_KEY);
    tx.oncomplete = () => resolve();
    tx.onerror = () => resolve();
  });
}

/** Decides whether this tab should warm. The record is the cross-tab lock:
    another tab's in-flight build, or a completed build from this same server
    run, both mean there is nothing to do. A record from a previous run is
    ignored — that server may be serving different content. */
export function shouldWarm(record: WarmRecord | null, runId: string): boolean {
  if (!record) return true;
  if (record.runId !== runId) return true;
  if (record.state === "done") return false;
  return Date.now() - record.at > STALE_BUILD_MS;
}

function offscreenContainer(): HTMLElement {
  const el = document.createElement("div");
  el.id = "system-cy";
  el.setAttribute("data-system-graph", "");
  // Off-viewport rather than hidden: the canvas needs real dimensions, and
  // display:none or visibility:hidden give it none.
  el.style.cssText =
    "position:fixed;left:-10000px;top:0;width:1200px;height:800px;pointer-events:none;opacity:0;";
  document.body.appendChild(el);
  return el;
}

let warming = false;
// Held so cancel removes THIS element. The real graph route mounts a
// container with the same id, and removing by id could take that one out.
let warmContainer: HTMLElement | null = null;

/** Aborts an in-flight warm and releases its WebGL context. Called when the
    reader opens the graph for real — that mount needs the engine, and the
    engine holds a single instance. */
export function cancelWarm(): void {
  if (!warming) return;
  warming = false;
  teardownGraph("docs");
  warmContainer?.remove();
  warmContainer = null;
}

export function isWarming(): boolean {
  return warming;
}

/**
 * Warms the docs-graph layout once per server run.
 *
 * Resolves when the warm finishes, is skipped, or is cancelled — never
 * rejects, because a failed warm must not surface anywhere: the graph still
 * works, it is just as slow as it was before.
 */
export async function warmDocsGraph(runId: string): Promise<"warmed" | "skipped" | "cancelled"> {
  if (warming || !runId) return "skipped";

  const db = await openDB();
  if (!db) return "skipped";

  const record = await readRecord(db);
  if (!shouldWarm(record, runId)) return "skipped";

  await writeRecord(db, { runId, state: "building", at: Date.now() });

  warming = true;
  const container = offscreenContainer();
  warmContainer = container;

  try {
    await mountGraph(container, "docs");
    // mountGraph resolves once the engine is mounted; the layout settles
    // asynchronously after that, and the engine writes the cache itself on
    // its first settle. Poll its own simulation flag rather than duplicating
    // that bookkeeping here.
    await waitForSettle();
  } catch {
    // Any failure leaves the record as "building" until the takeover window
    // expires, which is the correct outcome: retry later, do not hammer.
    cancelWarm();
    return "cancelled";
  }

  if (!warming) return "cancelled"; // cancelled while settling
  await writeRecord(db, { runId, state: "done", at: Date.now() });
  cancelWarm();
  return "warmed";
}

/** Resolves once the layout has settled.
 *
 * "Not running" alone is not settled: the simulation has not started yet in
 * the first moments after mount, so polling that flag straight away reports
 * done before any layout has happened. Settlement therefore requires having
 * seen it running first, or the grace period elapsing with no start at all
 * (a cache hit replays with no simulation). */
function waitForSettle(): Promise<void> {
  return new Promise((resolve) => {
    const started = Date.now();
    const GRACE_MS = 2500;
    let sawRunning = false;

    const poll = setInterval(() => {
      const core = (window as unknown as { GraphCore?: { simRunning?: () => boolean } }).GraphCore;
      const running = core?.simRunning?.() ?? false;
      if (running) sawRunning = true;

      const settled = sawRunning ? !running : Date.now() - started > GRACE_MS;
      // Give up after two minutes rather than hold a WebGL context forever
      // on a graph that never settles.
      if (!warming || settled || Date.now() - started > 120_000) {
        clearInterval(poll);
        resolve();
      }
    }, 400);
  });
}

/** Exported for tests: the lock decision is the part worth pinning, and it is
    pure — the IndexedDB plumbing around it is not. */
export const __shouldWarmForTest = shouldWarm;
