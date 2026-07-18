// Production build: bundles src/main.tsx, copies the carried public/ assets,
// and writes dist/index.html pointing at the bundled entry. Deterministic
// output (fixed asset names, no content hashing) so `make frontend` produces
// a byte-identical dist/ across runs — the drift gate depends on that.
import { cp, mkdir, rm } from "node:fs/promises";
import { readFileSync, writeFileSync } from "node:fs";

const outdir = "dist";

await rm(outdir, { recursive: true, force: true });
await mkdir(outdir, { recursive: true });

const result = await Bun.build({
  entrypoints: ["src/main.tsx"],
  outdir: `${outdir}/assets`,
  target: "browser",
  minify: true,
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
