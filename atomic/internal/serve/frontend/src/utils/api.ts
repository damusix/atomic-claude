// utils/api — the one shared FetchEngine instance every component talks to.
// Resilience (retry, request dedupe) is engine config, not hand-rolled.
// Never call `fetch` directly anywhere in src/ — go through `useApi()` (React
// components) or the exported `api` instance (non-React code, wrapped in
// `attempt()`) instead. See frontend/CLAUDE.md.
import { FetchEngine } from "@logosdx/fetch";
import { createFetchContext } from "@logosdx/react";

// FetchEngine requires an absolute baseUrl (it validates with `new URL()`,
// which throws on a bare relative path) — same-origin, so this always
// resolves correctly regardless of where the SPA is served from.
export const api = new FetchEngine({
  baseUrl: `${window.location.origin}/api`,
  defaultType: "json",
  retry: { maxAttempts: 3, baseDelay: 150 },
  dedupePolicy: true,
});

export const [ApiProvider, useApi] = createFetchContext(api);
