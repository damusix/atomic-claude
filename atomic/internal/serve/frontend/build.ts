// Production build: bundles src/main.tsx, copies the carried public/ assets,
// and writes dist/index.html pointing at the bundled entry. dist/ is
// gitignored; fixed asset names (no content hashing) keep the go:embed paths
// and the served URLs stable across builds.
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

const html = readFileSync("index.html", "utf8");
writeFileSync(`${outdir}/index.html`, html);

console.log(`build complete: ${outdir}/`);
