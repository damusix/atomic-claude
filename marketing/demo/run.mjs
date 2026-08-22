#!/usr/bin/env node
// Records the atomic serve feature tour as one video.
//
//   node marketing/demo/run.mjs                       full tour against $DEMO_ROOT
//   node marketing/demo/run.mjs --scene docs,schema   a subset, no concat
//   node marketing/demo/run.mjs --url http://127.0.0.1:4500   drive a server you started
//
// Flags
//   --root <dir>      directory to serve (default: env DEMO_ROOT, else ~/projects/noorm)
//   --bin <path>      atomic binary (default: bin/atomic relative to the repo root)
//   --url <base>      skip spawning; drive an already-running instance
//   --port <n>        port for the spawned server (default 4317)
//   --scene <a,b,c>   run only these scenes, in this order (default: all)
//   --no-concat       leave per-scene files in out/, skip the final mp4
//   --keep            don't delete per-scene intermediates after concat
//
// Each scene is one Playwright browser context with recordVideo, so its webm
// is self-contained: title card, interactions, outro beat. The chat scene
// additionally spawns two VHS tapes in parallel and composes the three
// recordings side by side. ffmpeg then transcodes every scene to a common
// 1920x1080 H.264 stream and concatenates.
//
// Requires: node_modules/playwright (npm ci at repo root), a cached Chromium,
// vhs and ffmpeg on PATH (brew install vhs ffmpeg).

import { chromium } from 'playwright';
import { spawn, spawnSync } from 'node:child_process';
import { mkdirSync, renameSync, rmSync, writeFileSync, existsSync } from 'node:fs';
import { join, dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { homedir } from 'node:os';
import { install } from './lib/overlay.mjs';
import { SCENES } from './scenes/index.mjs';

const HERE = dirname(fileURLToPath(import.meta.url));
const REPO = resolve(HERE, '..', '..');
const OUT = join(HERE, 'out');

const args = parseArgs(process.argv.slice(2));
const root = args.root ?? process.env.DEMO_ROOT ?? join(homedir(), 'projects', 'noorm');
const bin = args.bin ?? join(REPO, 'bin', 'atomic');
const port = Number(args.port ?? 4317);
const only = args.scene ? args.scene.split(',').map(s => s.trim()) : null;
const concat = !args['no-concat'] && !only;

mkdirSync(OUT, { recursive: true });

let server = null;
let base = args.url;
if (!base) {
  if (!existsSync(bin)) fail(`no binary at ${bin} — run: make -C atomic build`);
  base = `http://127.0.0.1:${port}`;
  server = spawn(bin, ['serve', '--port', String(port)], { cwd: root, stdio: ['ignore', 'pipe', 'pipe'] });
  server.stderr.on('data', d => process.stderr.write(`[serve] ${d}`));
  await waitFor(`${base}/api/status`, 15000);
  console.log(`serving ${root} at ${base}`);
}

const browser = await chromium.launch({
  args: ['--ignore-gpu-blocklist', '--enable-gpu-rasterization', '--force-device-scale-factor=1'],
});

const produced = [];
try {
  const list = only ? only.map(n => SCENES.find(s => s.name === n) ?? fail(`unknown scene ${n}`)) : SCENES;
  for (const [i, scene] of list.entries()) {
    const idx = String(i + 1).padStart(2, '0');
    console.log(`▶ ${idx} ${scene.name}`);
    const file = await record(scene, idx);
    produced.push(file);
    console.log(`  ✓ ${file}`);
  }
} finally {
  await browser.close();
  server?.kill('SIGTERM');
}

if (concat) {
  const final = join(OUT, 'demo.mp4');
  concatenate(produced, final);
  if (!args.keep) produced.forEach(f => rmSync(f, { force: true }));
  console.log(`\n${final}`);
}

async function record(scene, idx) {
  const viewport = scene.viewport ?? { width: 1920, height: 1080 };
  const ctx = await browser.newContext({
    viewport,
    recordVideo: { dir: OUT, size: viewport },
    colorScheme: 'dark',
  });
  const page = await ctx.newPage();
  await install(page);
  page.on('pageerror', e => console.warn(`  [pageerror] ${e.message}`));

  if (scene.tapes) resetRoom(scene.room);
  const side = scene.tapes ? runTapes(scene.tapes) : null;
  try {
    await scene.run(page, { base, root, idx });
  } finally {
    const raw = await page.video().path();
    await ctx.close();
    const webm = join(OUT, `${idx}-${scene.name}.webm`);
    renameSync(raw, webm);
    if (side) {
      const tapes = await side;
      const composed = join(OUT, `${idx}-${scene.name}.composed.mp4`);
      compose(webm, tapes, composed, viewport);
      rmSync(webm, { force: true });
      return composed;
    }
    return webm;
  }
}

// The room log under ~/.atomic/rooms/ outlives daemon restarts and room
// closes, and the roster in bus.json rehydrates members from earlier runs.
// Both would put last week's traffic in the recording, so the demo's own
// room is dropped, the daemon stopped, and its log removed. Only the demo
// room is touched.
function resetRoom(room) {
  if (!room) return;
  spawnSync(bin, ['bus', 'close', room], { stdio: 'ignore' });
  spawnSync(bin, ['bus', 'stop'], { stdio: 'ignore' });
  rmSync(join(homedir(), '.atomic', 'rooms', `${room}.log`), { force: true });
}

function runTapes(tapes) {
  return Promise.all(tapes.map(t => new Promise((res, rej) => {
    const tape = join(HERE, 'tapes', t);
    const out = join(OUT, t.replace(/\.tape$/, '.mp4'));
    const p = spawn('vhs', [tape], {
      cwd: HERE,
      env: { ...process.env, DEMO_BIN: bin, DEMO_ROOT: root, DEMO_OUT: OUT },
      stdio: ['ignore', 'inherit', 'inherit'],
    });
    p.on('exit', c => c === 0 ? res(out) : rej(new Error(`vhs ${t} exited ${c}`)));
  })));
}

// browser | (tape A over tape B), all cut to the shortest input, onto a
// 1920x1080 canvas so the concat step sees one stream shape.
function compose(webm, tapes, out, viewport) {
  const termW = 1920 - viewport.width;
  const termH = Math.floor(1080 / tapes.length);
  const inputs = [webm, ...tapes].flatMap(f => ['-i', f]);
  const scaled = tapes.map((_, i) => `[${i + 1}:v]scale=${termW}:${termH}:force_original_aspect_ratio=decrease,pad=${termW}:${termH}:(ow-iw)/2:(oh-ih)/2:color=#0c0c0e,fps=30[t${i}]`);
  const stack = tapes.length > 1 ? `${tapes.map((_, i) => `[t${i}]`).join('')}vstack=inputs=${tapes.length}[term]` : `[t0]copy[term]`;
  const filter = [
    `[0:v]scale=${viewport.width}:1080:force_original_aspect_ratio=decrease,pad=${viewport.width}:1080:(ow-iw)/2:(oh-ih)/2:color=#0c0c0e,fps=30[web]`,
    ...scaled, stack,
    `[web][term]hstack=inputs=2[v]`,
  ].join(';');
  ffmpeg(['-y', ...inputs, '-filter_complex', filter, '-map', '[v]', '-shortest', '-c:v', 'libx264', '-pix_fmt', 'yuv420p', '-crf', '18', '-preset', 'medium', out]);
}

function concatenate(files, out) {
  const norm = files.map((f, i) => {
    const o = join(OUT, `_n${i}.mp4`);
    ffmpeg(['-y', '-i', f, '-vf', 'scale=1920:1080:force_original_aspect_ratio=decrease,pad=1920:1080:(ow-iw)/2:(oh-ih)/2:color=#0c0c0e,fps=30,format=yuv420p', '-c:v', 'libx264', '-crf', '18', '-preset', 'medium', '-an', o]);
    return o;
  });
  const list = join(OUT, '_concat.txt');
  writeFileSync(list, norm.map(f => `file '${f}'`).join('\n') + '\n');
  ffmpeg(['-y', '-f', 'concat', '-safe', '0', '-i', list, '-c', 'copy', out]);
  norm.forEach(f => rmSync(f, { force: true }));
  rmSync(list, { force: true });
}

function ffmpeg(argv) {
  const r = spawnSync('ffmpeg', ['-hide_banner', '-loglevel', 'error', ...argv], { stdio: 'inherit' });
  if (r.status !== 0) fail(`ffmpeg failed: ffmpeg ${argv.join(' ')}`);
}

async function waitFor(url, ms) {
  const t0 = Date.now();
  while (Date.now() - t0 < ms) {
    try { if ((await fetch(url)).ok) return; } catch {}
    await new Promise(r => setTimeout(r, 200));
  }
  fail(`server at ${url} did not answer within ${ms}ms`);
}

function parseArgs(argv) {
  const o = {};
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (!a.startsWith('--')) continue;
    const k = a.slice(2);
    const next = argv[i + 1];
    if (next === undefined || next.startsWith('--')) o[k] = true; else { o[k] = next; i++; }
  }
  return o;
}

function fail(msg) { console.error(msg); process.exit(1); }
