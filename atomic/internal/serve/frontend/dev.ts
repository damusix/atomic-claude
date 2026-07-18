// Dev server: serves the workspace with Bun's built-in bundler/HMR and
// proxies everything it doesn't own (/api, /graph, /code, /events, /search,
// /healthz, /static) to a running `atomic serve` backend. Target configurable
// via ATOMIC_SERVE_URL (see bunfig.toml).
const backend = process.env.ATOMIC_SERVE_URL ?? "http://127.0.0.1:4000";
const proxiedPrefixes = ["/api", "/graph", "/code", "/events", "/search", "/healthz", "/static", "/file", "/nav"];

const server = Bun.serve({
  port: Number(process.env.PORT ?? 5173),
  development: { hmr: true },
  routes: {
    "/*": (req) => {
      const url = new URL(req.url);
      if (proxiedPrefixes.some((prefix) => url.pathname === prefix || url.pathname.startsWith(`${prefix}/`) || url.pathname.startsWith(`${prefix}?`))) {
        return fetch(new URL(`${url.pathname}${url.search}`, backend), req);
      }
      return new Response(Bun.file("index.html"));
    },
  },
});

console.log(`dev server: http://localhost:${server.port} (proxying ${proxiedPrefixes.join(", ")} -> ${backend})`);
