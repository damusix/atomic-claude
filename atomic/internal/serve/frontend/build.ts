// dist/ is gitignored and embedded whole. Anything referenced by URL keeps a
// fixed name; only the ELK chunks, reached solely through their entry, hash.
import { cp, mkdir, rm } from "node:fs/promises";
import { readFileSync, writeFileSync } from "node:fs";

const outdir = "dist";

await rm(outdir, { recursive: true, force: true });
await mkdir(outdir, { recursive: true });

const result = await Bun.build({
  entrypoints: ["src/main.tsx"],
  outdir: `${outdir}/assets`,
  target: "browser",
  // Identifier minification's renamer has a non-deterministic tie-breaker
  // at this dependency-graph size (react-router + @ark-ui/react pull in
  // enough modules that symbol-name collisions resolve differently across
  // otherwise-identical builds) — verified byte-for-byte on repeat builds
  // with identifiers off. Whitespace/syntax minification stays on for most
  // of the size win; only the renamer is disabled.
  minify: { whitespace: true, syntax: true, identifiers: false },
  naming: "[name].[ext]",
});

if (!result.success) {
  for (const log of result.logs) {
    console.error(log);
  }
  process.exit(1);
}

await cp("public", outdir, { recursive: true });

// layout-elk bundles a copy of mermaid's internals: upgrade both dependencies
// together. utils/mermaidElk.ts imports the ELK entry by URL after window load.
await cp("node_modules/mermaid/dist/mermaid.min.js", `${outdir}/vendor/mermaid.min.js`);

const elk = await Bun.build({
  entrypoints: ["mermaid-layout-elk.entry.ts"],
  outdir: `${outdir}/vendor/mermaid-layout-elk`,
  target: "browser",
  format: "esm",
  splitting: true,
  // Unlike main.js, renaming here is deterministic: repeat builds hash identically.
  minify: true,
  naming: { entry: "mermaid-layout-elk.[ext]", chunk: "chunk-[hash].[ext]" },
});

if (!elk.success) {
  for (const log of elk.logs) {
    console.error(log);
  }
  process.exit(1);
}

const html = readFileSync("index.html", "utf8");
writeFileSync(`${outdir}/index.html`, html);

console.log(`build complete: ${outdir}/`);
