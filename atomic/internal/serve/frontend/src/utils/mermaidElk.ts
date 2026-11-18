// utils/mermaidElk — loads the ELK layout that build.ts writes to dist/vendor/mermaid-layout-elk/
// once per page and registers it; resolves false so the caller falls back to dagre.
import { attempt } from "@logosdx/utils";

const ELK_PATH = "/vendor/mermaid-layout-elk/mermaid-layout-elk.js";

let pending: Promise<boolean> | null = null;

function afterWindowLoad(): Promise<void> {
  if (document.readyState === "complete") return Promise.resolve();
  return new Promise((resolve) => window.addEventListener("load", () => resolve(), { once: true }));
}

type Importer = (url: string) => Promise<unknown>;

const importFromBrowser: Importer = (url) => import(url);

async function load(register: (loaders: unknown[]) => void, importModule: Importer): Promise<boolean> {
  // The module bundles a mermaid copy whose window "load" listener re-renders every
  // .mermaid element into a syntax error; evaluated after that event, it never runs.
  await afterWindowLoad();
  // Built at runtime so the Bun bundler leaves the import to the browser.
  const url = new URL(ELK_PATH, window.location.origin).href;
  const [mod, err] = await attempt(() => importModule(url));
  const loaders: unknown = mod && typeof mod === "object" && "default" in mod ? mod.default : undefined;
  if (err || !Array.isArray(loaders)) return false;
  register(loaders);
  return true;
}

export function registerElkLayouts(
  register: (loaders: unknown[]) => void,
  importModule: Importer = importFromBrowser,
): Promise<boolean> {
  pending ??= load(register, importModule);
  return pending;
}

export function __resetElkForTest(): void {
  pending = null;
}
