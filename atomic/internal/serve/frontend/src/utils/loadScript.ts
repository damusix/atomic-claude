// loadScript — lazy-loads a carried vendor <script> (Cytoscape, mermaid) at
// most once per src, resolving when the tag fires "load". Mirrors
// templates/layout.html's lazy-load comment ("mermaid library itself is
// loaded lazily by fragments that contain diagrams") — the rail mini-graph
// and the mermaid mount effect both need this, so it lives once in utils/
// rather than duplicated per consumer.
const loaded = new Map<string, Promise<void>>();

export function loadScript(src: string): Promise<void> {
  const cached = loaded.get(src);
  if (cached) return cached;

  const promise = new Promise<void>((resolve, reject) => {
    const existing = document.querySelector<HTMLScriptElement>(`script[src="${src}"]`);
    if (existing) {
      // The tag can have already fired "load"/"error" before this call ever
      // ran (a cache miss for a src some earlier, unrelated call already
      // settled) — a fresh listener on a past event never fires, hanging
      // this promise forever. `dataset.loaded` is stamped by the listener
      // below the moment the tag settles, so a cache miss can check it
      // instead of re-listening blind.
      if (existing.dataset.loaded === "true") {
        resolve();
        return;
      }
      if (existing.dataset.loaded === "error") {
        reject(new Error(`failed to load ${src}`));
        return;
      }
      existing.addEventListener("load", () => {
        existing.dataset.loaded = "true";
        resolve();
      });
      existing.addEventListener("error", () => {
        existing.dataset.loaded = "error";
        reject(new Error(`failed to load ${src}`));
      });
      return;
    }
    const el = document.createElement("script");
    el.src = src;
    el.addEventListener("load", () => {
      el.dataset.loaded = "true";
      resolve();
    });
    el.addEventListener("error", () => {
      el.dataset.loaded = "error";
      reject(new Error(`failed to load ${src}`));
    });
    document.head.appendChild(el);
  });

  loaded.set(src, promise);
  return promise;
}

// Test-only reset — clears the load cache between test cases.
export function __resetLoadScriptCacheForTest(): void {
  loaded.clear();
}
