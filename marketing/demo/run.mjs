#!/usr/bin/env node
// Records the atomic serve feature tour as one video, one scene at a time.
//
//   node marketing/demo/run.mjs                  record scenes missing from out/scenes/, then stitch
//   node marketing/demo/run.mjs --scene search   re-record just these scenes, then stitch
//   node marketing/demo/run.mjs --force          re-record everything
//   node marketing/demo/run.mjs --stitch         only concatenate what is already in out/scenes/
//
// Flags
//   --root <dir>      directory to serve (default: env DEMO_ROOT, else ~/projects/noorm)
//   --bin <path>      atomic binary (default: bin/atomic relative to the repo root)
//   --url <base>      skip spawning; drive an already-running instance
//   --port <n>        port for the spawned server (default 4317)
//   --scene <a,b,c>   record only these scenes (comma-separated names from scenes/index.mjs)
//   --force           record every scene even if its file exists
//   --stitch          skip recording
//   --no-stitch       skip the final concat
//
// Layout of out/
//   raw/<scene>.webm            Playwright capture (and <scene>.<tape>.mp4 for tape scenes)
//   scenes/<scene>.mp4          normalized 1920x1080 H.264 30fps, speed applied
//   demo.mp4                    every scene in scenes/index.mjs order
//
// A scene is { name, run(page, ctx), viewport?, tapes?, room?, speed?, warm? }.
// warm(page, ctx) runs first in a throwaway page of the same browser context
// whose video is discarded; use it to fill caches (the graph replays its
// layout from IndexedDB) so the recorded page opens on the finished state.
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
const RAW = join(OUT, 'raw');
const DONE = join(OUT, 'scenes');

const args = parseArgs(process.argv.slice(2));
const root = args.root ?? process.env.DEMO_ROOT ?? join(homedir(), 'projects', 'noorm');
const bin = args.bin ?? join(REPO, 'bin', 'atomic');
const port = Number(args.port ?? 4317);

for (const d of [RAW, DONE]) mkdirSync(d, { recursive: true });

const sceneFile = s => join(DONE, `${s.name}.mp4`);
let targets = [];
if (!args.stitch) {
  if (args.scene) targets = args.scene.split(',').map(n => SCENES.find(s => s.name === n.trim()) ?? fail(`unknown scene ${n}`));
  else targets = SCENES.filter(s => args.force || !existsSync(sceneFile(s)));
}

if (targets.length) {
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
    args: ['--use-gl=angle', '--ignore-gpu-blocklist', '--enable-gpu-rasterization', '--force-device-scale-factor=1'],
  });
  try {
    for (const scene of targets) {
      console.log(`▶ ${scene.name}`);
      const raw = await record(browser, scene, base);
      normalize(raw, sceneFile(scene), scene.speed ?? 1);
      console.log(`  ✓ ${sceneFile(scene)}`);
    }
  } finally {
    await browser.close();
    server?.kill('SIGTERM');
  }
} else if (!args.stitch) {
  console.log('nothing to record (every scene present; use --force or --scene)');
}

if (!args['no-stitch']) {
  const present = SCENES.filter(s => existsSync(sceneFile(s)));
  const missing = SCENES.filter(s => !existsSync(sceneFile(s))).map(s => s.name);
  if (missing.length) console.log(`stitching without: ${missing.join(', ')}`);
  if (!present.length) fail('no scenes to stitch');
  const final = join(OUT, 'demo.mp4');
  concatenate(present.map(sceneFile), final);
  console.log(`\n${final}`);
}

async function record(browser, scene, base) {
  const viewport = scene.viewport ?? { width: 1920, height: 1080 };
  const ctx = await browser.newContext({ viewport, recordVideo: { dir: RAW, size: viewport }, colorScheme: 'dark' });
  const env = { base, root };

  if (scene.warm) {
    const w = await ctx.newPage();
    await scene.warm(w, env);
    const junk = await w.video().path();
    await w.close();
    rmSync(junk, { force: true });
  }

  const page = await ctx.newPage();
  await install(page);
  page.on('pageerror', e => console.warn(`  [pageerror] ${e.message}`));

  if (scene.tapes) resetRoom(scene.room);
  const side = scene.tapes ? runTapes(scene) : null;
  let failure = null;
  try {
    await scene.run(page, env);
  } catch (e) {
    failure = e;
    await page.screenshot({ path: join(RAW, `${scene.name}.failed.png`) }).catch(() => {});
  }
  const captured = await page.video().path();
  await ctx.close();
  const webm = join(RAW, `${scene.name}.webm`);
  renameSync(captured, webm);
  if (side) await side.catch(e => { failure ??= e; });
  if (failure) fail(`scene ${scene.name} failed: ${failure.message.split('\n')[0]}\n  capture kept at ${webm}; screenshot at ${join(RAW, `${scene.name}.failed.png`)}`);
  if (side) {
    const tapes = await side;
    const composed = join(RAW, `${scene.name}.composed.mp4`);
    compose(webm, tapes, composed, viewport);
    return composed;
  }
  return webm;
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

function runTapes(scene) {
  return Promise.all(scene.tapes.map(t => new Promise((res, rej) => {
    const out = join(RAW, `${scene.name}.${t.replace(/\.tape$/, '.mp4')}`);
    const p = spawn('vhs', [join(HERE, 'tapes', t), '-o', out], {
      cwd: HERE,
      env: { ...process.env, DEMO_BIN: bin, DEMO_ROOT: root },
      stdio: ['ignore', 'inherit', 'inherit'],
    });
    p.on('exit', c => c === 0 ? res(out) : rej(new Error(`vhs ${t} exited ${c}`)));
  })));
}

// browser | (tape A over tape B), cut to the shortest input, on a 1920x1080
// canvas so normalize() sees the same shape as a plain capture.
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

function normalize(src, out, speed) {
  const vf = [
    'scale=1920:1080:force_original_aspect_ratio=decrease',
    'pad=1920:1080:(ow-iw)/2:(oh-ih)/2:color=#0c0c0e',
    speed !== 1 ? `setpts=PTS/${speed}` : null,
    'fps=30', 'format=yuv420p',
  ].filter(Boolean).join(',');
  ffmpeg(['-y', '-ss', '0.4', '-i', src, '-vf', vf, '-c:v', 'libx264', '-crf', '18', '-preset', 'medium', '-an', out]);
}

function concatenate(files, out) {
  const list = join(OUT, '_concat.txt');
  writeFileSync(list, files.map(f => `file '${f}'`).join('\n') + '\n');
  ffmpeg(['-y', '-f', 'concat', '-safe', '0', '-i', list, '-c', 'copy', out]);
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
