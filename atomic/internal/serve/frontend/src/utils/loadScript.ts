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
      existing.addEventListener("load", () => resolve());
      existing.addEventListener("error", () => reject(new Error(`failed to load ${src}`)));
      return;
    }
    const el = document.createElement("script");
    el.src = src;
    el.addEventListener("load", () => resolve());
    el.addEventListener("error", () => reject(new Error(`failed to load ${src}`)));
    document.head.appendChild(el);
  });

  loaded.set(src, promise);
  return promise;
}

// Test-only reset — clears the load cache between test cases.
export function __resetLoadScriptCacheForTest(): void {
  loaded.clear();
}
