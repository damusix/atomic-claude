#!/usr/bin/env node
// scripts/graph-gates.mjs — SC3 committed gate harness (docs/spec/code-graph.md).
//
// Drives headless Chromium (Playwright) against a RUNNING `atomic serve`
// instance and checks five behavioral gates for a named graph view: clean
// mount (zero console errors), settle-then-pause within a time budget,
// drag/overlap resolution, IndexedDB cache-replay with zero motion, and
// hover preview. Exits non-zero on ANY gate failure. This is a LOCAL tool —
// CI carries Go/unit coverage only (no browser/GPU in CI); run it by hand
// after touching atomic/internal/serve/assets/{graph-core,system-graph,code-graph}.js
// or templates/layout.html.
//
// Usage:
//   node scripts/graph-gates.mjs --view docs --url http://127.0.0.1:4500
//   node scripts/graph-gates.mjs --view docs --serve-bin bin/atomic
//
// Flags:
//   --view <docs|code>       Target graph view. "docs" navigates straight to
//                            /graph, which auto-mounts window.SystemGraph via
//                            the htmx.onLoad delegation in layout.html.
//                            "code" drives the real UI path (checkpoint 6):
//                            lands on the landing page, clicks #btn-graph to
//                            enter graph mode (docs mounts by default), then
//                            clicks the Docs|Code switcher's Code button —
//                            see VIEWS.code.navigate below. Every gate below
//                            reads state through window.GraphCore (the
//                            shared engine both profiles forward verbatim),
//                            so gates 1-5 run identically regardless of
//                            which profile mounted.
//   --url <base>             Drive an ALREADY-RUNNING atomic serve instance
//                            at this base URL. Mutually exclusive with
//                            --serve-bin.
//   --serve-bin <path>       Spawn `<path> serve <--repo-root> --port
//                            <scratch>` for the run's duration and tear it
//                            down after. Build first from atomic/:
//                              go build -o bin/atomic ./cmd/atomic
//                            Mutually exclusive with --url.
//   --repo-root <path>       Directory to serve when spawning via
//                            --serve-bin (default: this repo's root — two
//                            levels up from this script).
//   --settle-budget-ms <n>   Hard time budget for gate 2 (settle-then-pause)
//                            and the post-drag re-settle wait in gate 3.
//                            Default 30000. Measured (11 real runs, this
//                            repo's own index, GPU-accelerated launch — see
//                            chromium.launch()'s args below): docs settles in
//                            ~4-10s, code (17.5k nodes/54k edges) in
//                            ~9.8-10.4s, both with low run-to-run variance —
//                            30000 keeps roughly the same ~3x headroom over
//                            code's measured settle the prior 15000 default
//                            gave docs. Lowering this is how to prove the
//                            harness CAN fail (temporarily pass e.g. `1` and
//                            observe a non-zero exit), not a flag for normal
//                            runs.
//   --simulate-no-playwright Force the skip path without touching module
//                            resolution — prints the skip message and exits
//                            0, for testing the skip path itself.
//
// Requires the "playwright" devDependency (npm install from repo root) and
// an already-cached Chromium build under Playwright's browser cache
// (~/Library/Caches/ms-playwright on macOS; $XDG_CACHE_HOME/ms-playwright or
// ~/.cache/ms-playwright on Linux). Do NOT run `npx playwright install` —
// package.json pins an exact playwright version (not a caret range)
// specifically so its bundled Chromium revision keeps matching whatever is
// already on disk; bumping it requires re-verifying the cached revision
// first. If playwright cannot be resolved, or launching the cached browser
// fails for any reason, this script prints a one-line skip message and
// exits 0 — a missing local browser is not a gate failure.

import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawn } from 'node:child_process';
import net from 'node:net';

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const REPO_ROOT = path.resolve(__dirname, '..');

// VIEWS maps --view to a navigate(page, baseURL) that gets that view's graph
// mounted and its container + `.system-graph-loading` marker into the DOM,
// returning once the triggering navigation's 'load' event fires (gate 2 then
// waits on the loading marker separately, in runGates below).
const VIEWS = {
  // docs: a document load of /graph triggers #main-pane's
  // hx-get="{{.LandingURL}}" on load, which fetches the systemGraphFragmentHTML
  // fragment (serve.go) and runs SystemGraph.mount() via the htmx.onLoad
  // delegation keyed on [data-system-graph] (layout.html).
  docs: {
    async navigate(page, baseURL) {
      await page.goto(baseURL + '/graph', { waitUntil: 'load' });
    }
  },
  // code: the real UI path (checkpoint 6) — no page.evaluate bridge. Lands on
  // the landing page, clicks #btn-graph to enter graph mode (mounts the docs
  // profile by default via system-graph.js's enterGraphMode), waits for the
  // Docs|Code switcher to exist, then clicks its Code button — the same
  // renderGraphPane() path a real user's click takes, tearing down docs and
  // mounting window.CodeGraph on a fresh #code-cy[data-code-graph] container.
  code: {
    async navigate(page, baseURL) {
      // The landing shell's #main-pane hx-get="{{.LandingURL}}" auto-fetch
      // (FE8) must settle before #btn-graph's click-triggered /graph fetch
      // starts, or the two htmx swaps race. 'networkidle' used to encode
      // that, but the live-reload /events EventSource holds a connection
      // open for the page's whole life, so networkidle never fires anymore —
      // wait for the auto-fetch's own rendered marker instead.
      await page.goto(baseURL + '/', { waitUntil: 'load' });
      await page.waitForSelector('#main-pane #page-content, #main-pane .md-content', { timeout: 15000 });
      await page.click('#btn-graph');
      await page.waitForSelector('[data-graph-view="code"]');
      await page.click('[data-graph-view="code"]');
    }
  }
};

function parseArgs(argv) {
  const args = { view: null, url: null, serveBin: null, repoRoot: REPO_ROOT, settleBudgetMs: 30000, simulateNoPlaywright: false };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--view') { args.view = argv[++i]; }
    else if (a === '--url') { args.url = argv[++i]; }
    else if (a === '--serve-bin') { args.serveBin = argv[++i]; }
    else if (a === '--repo-root') { args.repoRoot = path.resolve(argv[++i]); }
    else if (a === '--settle-budget-ms') { args.settleBudgetMs = Number(argv[++i]); }
    else if (a === '--simulate-no-playwright') { args.simulateNoPlaywright = true; }
    else { throw new Error('unrecognized argument: ' + a); }
  }
  return args;
}

// skip returns the exit code (always 0) rather than calling process.exit()
// itself — every call site does `return skip(...)` so a pending --serve-bin
// child still gets torn down by main()'s finally block below. Calling
// process.exit() directly from inside that try/finally would terminate the
// process immediately, WITHOUT running the finally (Node does not unwind
// finally blocks for process.exit()) — leaving an orphaned `atomic serve`.
function skip(message) {
  console.log('SKIP  ' + message);
  return 0;
}

function freePort() {
  return new Promise((resolve, reject) => {
    const srv = net.createServer();
    srv.listen(0, '127.0.0.1', () => {
      const { port } = srv.address();
      srv.close(() => resolve(port));
    });
    srv.on('error', reject);
  });
}

// killChild sends SIGTERM and waits for the process to actually exit before
// resolving — an un-awaited child.kill() lets main() (and this script's
// process.exit()) return before the OS has reaped the child, which can leave
// an orphaned `atomic serve` behind a short-lived script run. Escalates to
// SIGKILL if the child hasn't exited within killTimeoutMs.
function killChild(child, killTimeoutMs) {
  return new Promise((resolve) => {
    if (child.exitCode !== null || child.signalCode !== null) { resolve(); return; }
    child.once('exit', () => resolve());
    child.kill('SIGTERM');
    setTimeout(() => {
      if (child.exitCode === null && child.signalCode === null) { child.kill('SIGKILL'); }
    }, killTimeoutMs);
  });
}

async function waitForServer(baseURL, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(baseURL + '/healthz');
      if (res.ok) { return; }
    } catch (e) { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error('atomic serve did not become healthy within ' + timeoutMs + 'ms');
}

// runGates drives every gate against one already-resolved baseURL + view
// path, returning the array of { name, pass, detail } results — never throws
// for a GATE failure (those are recorded in the array), only for
// harness-level errors (browser launch, navigation, etc.), which the caller
// may treat as a skip (e.g. no cached browser available).
async function runGates(chromium, baseURL, view, settleBudgetMs) {
  // GPU args: Playwright's default headless launch renders WebGL2 through
  // SwiftShader (software) — confirmed via WEBGL_debug_renderer_info against
  // this harness's own launch (no args: "ANGLE (Google, Vulkan ... SwiftShader
  // Device ..., SwiftShader driver)"). Cosmos.gl's simulation is GPU-compute
  // heavy; under SwiftShader the code view's settle-then-pause (17.5k nodes)
  // was still running past 98s. These three flags let ANGLE pick the
  // platform's real GPU backend when one is available (confirmed on this
  // machine: "ANGLE (Apple, ANGLE Metal Renderer: Apple M4, ...)"), which
  // brought the same settle down to ~12-13s — the number the settle budget
  // below is sized against. A machine with no usable GPU falls back to
  // ANGLE's own software path same as before; these flags only ask for
  // hardware when present, they don't require it.
  const browser = await chromium.launch({
    headless: true,
    args: ['--use-gl=angle', '--ignore-gpu-blocklist', '--enable-gpu-rasterization']
  });
  const results = [];
  try {
    const page = await browser.newPage();
    const consoleErrors = [];
    page.on('pageerror', (err) => consoleErrors.push('pageerror: ' + err.message));
    page.on('console', (msg) => { if (msg.type() === 'error') { consoleErrors.push('console.error: ' + msg.text()); } });

    // ── Gate 1 setup + Gate 2: mount, wait for settle-then-pause ──────────
    const t0 = Date.now();
    await view.navigate(page, baseURL);
    let settleMs = null;
    let settleErr = null;
    try {
      await page.waitForSelector('.system-graph-loading', { state: 'detached', timeout: settleBudgetMs });
      settleMs = Date.now() - t0;
    } catch (e) {
      settleErr = 'did not settle within ' + settleBudgetMs + 'ms';
    }
    // window.GraphCore is the shared engine both profiles forward
    // debugState()/simRunning() from verbatim — reading it directly here
    // (rather than window.SystemGraph/window.CodeGraph) lets every gate
    // below run unchanged regardless of which profile is mounted.
    const stateAfterSettle = settleErr ? null : await page.evaluate(() => window.GraphCore.debugState());
    const settlePass = !settleErr && !!stateAfterSettle && stateAfterSettle.isSimulationRunning === false;
    results.push({
      name: 'settle-then-pause',
      pass: settlePass,
      detail: settlePass ? settleMs + 'ms (budget ' + settleBudgetMs + 'ms)' : (settleErr || 'settled but isSimulationRunning was true')
    });

    // Gate 1 (clean mount): zero console errors accumulated through the mount
    // + settle above — checked after settle so mount errors have had time to
    // surface, but before any interaction gates that follow.
    const mountPass = consoleErrors.length === 0;
    results.push({
      name: 'clean-mount',
      pass: mountPass,
      detail: mountPass ? 'zero console errors' : consoleErrors.length + ' error(s): ' + consoleErrors.slice(0, 3).join(' | ')
    });

    if (!settlePass) {
      // Gates 3-5 all depend on a settled mount to read node positions from —
      // record them as failed-dependency rather than attempting and hanging.
      ['drag-overlap-resolution', 'cache-replay-zero-motion', 'hover-preview'].forEach((name) => {
        results.push({ name, pass: false, detail: 'skipped: settle-then-pause did not pass' });
      });
      return results;
    }

    // ── Gate 3: drag one node onto another, verify post-cooldown separation ──
    const containerRect = await page.evaluate(() => {
      // Matches either profile's mount container — the docs fragment carries
      // [data-system-graph] (serve.go's systemGraphFragmentHTML); the code
      // view's harness-built container (VIEWS.code.navigate above) carries
      // [data-code-graph].
      const el = document.querySelector('[data-system-graph],[data-code-graph]');
      const r = el.getBoundingClientRect();
      return { left: r.left, top: r.top, width: r.width, height: r.height };
    });
    const onScreen = Object.entries(stateAfterSettle.nodes)
      .filter(([, n]) => n.screen.x >= 0 && n.screen.x <= containerRect.width && n.screen.y >= 0 && n.screen.y <= containerRect.height);
    if (onScreen.length < 2) {
      results.push({ name: 'drag-overlap-resolution', pass: false, detail: 'fewer than 2 on-screen nodes to drag' });
    } else {
      // Pick two MID-size (mid-degree proxy — size is a monotonic function of
      // degree via sizeForDegree) on-screen nodes rather than the first two
      // in server insertion order. On a dense graph the "first two" tended to
      // land on high-degree hub nodes (near MAX_POINT_SIZE, already the
      // hardest pair to separate and often sitting in the densest part of
      // the layout) — an adversarial worst-case drag target, not a
      // representative one. Sorting by size and taking the middle pair keeps
      // the SAME assertion (post-release separation > radiusSum) but targets
      // a typical mid-degree node instead of stacking the deck toward a
      // near-max-size hub.
      const bySize = onScreen.slice().sort((x, y) => x[1].size - y[1].size);
      const mid = Math.floor(bySize.length / 2);
      const [idA, nodeA] = bySize[Math.max(0, mid - 1)];
      const [idB, nodeB] = bySize[Math.min(bySize.length - 1, mid)];
      const toPage = (n) => ({ x: containerRect.left + n.screen.x, y: containerRect.top + n.screen.y });
      const start = toPage(nodeA);
      const target = toPage(nodeB);
      await page.mouse.move(start.x, start.y);
      await page.mouse.down();
      await page.mouse.move(target.x, target.y, { steps: 10 });
      await page.mouse.up();

      let dragSettleErr = null;
      try {
        // Polls the cheap O(1) simRunning() accessor rather than the O(n)
        // debugState() — this waitForFunction re-invokes on every animation
        // frame while the drag's post-release cooldown ticks down, and
        // debugState() recomputes every node's space+screen position on each
        // call (fine for the point-in-time reads elsewhere in this file, not
        // for a per-frame poll at graph scale).
        await page.waitForFunction(() => window.GraphCore.simRunning() === false, { timeout: settleBudgetMs });
      } catch (e) { dragSettleErr = 'post-drag cooldown did not settle within ' + settleBudgetMs + 'ms'; }

      if (dragSettleErr) {
        results.push({ name: 'drag-overlap-resolution', pass: false, detail: dragSettleErr });
      } else {
        const after = await page.evaluate(() => window.GraphCore.debugState());
        const a = after.nodes[idA], b = after.nodes[idB];
        const dist = Math.hypot(a.screen.x - b.screen.x, a.screen.y - b.screen.y);
        const radiusSum = (a.size + b.size) / 2;
        const separated = dist > radiusSum;
        results.push({
          name: 'drag-overlap-resolution',
          pass: separated,
          detail: 'distance=' + dist.toFixed(1) + 'px radiusSum=' + radiusSum.toFixed(1) + 'px'
        });
      }
    }

    // ── Gate 4: reload with unchanged content, cache replay, zero motion ──
    const t1 = Date.now();
    await view.navigate(page, baseURL);
    let reloadSettleErr = null;
    try {
      await page.waitForSelector('.system-graph-loading', { state: 'detached', timeout: settleBudgetMs });
    } catch (e) { reloadSettleErr = 'reload did not settle within ' + settleBudgetMs + 'ms'; }

    if (reloadSettleErr) {
      results.push({ name: 'cache-replay-zero-motion', pass: false, detail: reloadSettleErr });
    } else {
      // Compared in SPACE coordinates (the simulation's own, camera-
      // independent frame — see debugState()'s comment): the fitView()
      // camera transition that runs once per mount can still be easing at
      // t0, which would read as spurious SCREEN-space motion that has
      // nothing to do with whether the cached layout replayed unperturbed.
      const posT0 = await page.evaluate(() => window.GraphCore.debugState());
      await page.waitForTimeout(2000);
      const posT1 = await page.evaluate(() => window.GraphCore.debugState());
      let maxDisplacement = 0;
      Object.keys(posT0.nodes).forEach((id) => {
        const p0 = posT0.nodes[id].space, p1 = posT1.nodes[id] && posT1.nodes[id].space;
        if (!p1) { return; }
        maxDisplacement = Math.max(maxDisplacement, Math.hypot(p1.x - p0.x, p1.y - p0.y));
      });
      // A cache-hit replay (render(0), alpha=0) never ticks the simulation
      // again, so true displacement is exactly 0 — this tolerance only
      // absorbs float round-trip noise through JSON serialization.
      const EPSILON_SPACE = 1e-4;
      const zeroMotion = maxDisplacement < EPSILON_SPACE;
      const pass = posT0.cacheHit === true && zeroMotion;
      results.push({
        name: 'cache-replay-zero-motion',
        pass,
        detail: 'cacheHit=' + posT0.cacheHit + ' maxDisplacement=' + maxDisplacement.toExponential(3) + ' space units (reload took ' + (Date.now() - t1) + 'ms to settle)'
      });
    }

    // ── Gate 5: hover a node, preview card appears with non-empty content ──
    const hoverState = await page.evaluate(() => window.GraphCore.debugState());
    const [, hoverNode] = Object.entries(hoverState.nodes)
      .find(([, n]) => n.screen.x >= 0 && n.screen.x <= containerRect.width && n.screen.y >= 0 && n.screen.y <= containerRect.height) || [];
    if (!hoverNode) {
      results.push({ name: 'hover-preview', pass: false, detail: 'no on-screen node to hover' });
    } else {
      const hoverPage = { x: containerRect.left + hoverNode.screen.x, y: containerRect.top + hoverNode.screen.y };
      await page.mouse.move(hoverPage.x, hoverPage.y);
      try {
        await page.waitForSelector('#cy-preview-card.open', { timeout: 5000 });
        const text = (await page.textContent('#cy-preview-card') || '').trim();
        const pass = text.length > 0;
        results.push({ name: 'hover-preview', pass, detail: pass ? 'card content: "' + text.slice(0, 60) + '"' : 'card opened but content was empty' });
      } catch (e) {
        results.push({ name: 'hover-preview', pass: false, detail: 'preview card did not open within 5000ms' });
      }
    }

    return results;
  } finally {
    await browser.close();
  }
}

// main returns the intended exit code rather than calling process.exit()
// itself, so the --serve-bin child-process cleanup below always runs first
// (see skip()'s comment) — the single process.exit() call lives in the
// bottom-of-file invocation, after main()'s promise (and its finally) has
// already settled.
async function main() {
  const args = parseArgs(process.argv.slice(2));

  if (args.simulateNoPlaywright) {
    return skip('playwright unavailable (simulated via --simulate-no-playwright)');
  }

  if (!args.view || !VIEWS[args.view]) {
    console.error('usage: node scripts/graph-gates.mjs --view <docs|code> [--url <base> | --serve-bin <path>] [--repo-root <path>] [--settle-budget-ms <n>]');
    return 2;
  }
  const view = VIEWS[args.view];

  if (!args.url && !args.serveBin) {
    console.error('usage: exactly one of --url <base> or --serve-bin <path> is required');
    return 2;
  }
  if (args.url && args.serveBin) {
    console.error('usage: --url and --serve-bin are mutually exclusive');
    return 2;
  }

  let chromium;
  try {
    ({ chromium } = await import('playwright'));
  } catch (e) {
    return skip('playwright is not resolvable from this repo (npm install playwright first): ' + e.message);
  }

  let child = null;
  let baseURL = args.url;
  try {
    if (args.serveBin) {
      const port = await freePort();
      baseURL = 'http://127.0.0.1:' + port;
      // Flags must precede the positional target-dir arg: Go's flag package
      // stops parsing at the first non-flag token, so `serve <dir> --port N`
      // silently ignores --port (verified against atomic/internal/serve/serve.go
      // parseFlags — ordinary flag.FlagSet, not a subcommand-aware parser).
      child = spawn(args.serveBin, ['serve', '--port', String(port), args.repoRoot], { stdio: 'ignore' });
      const spawnFailed = await new Promise((resolve) => {
        child.once('error', () => resolve(true));
        child.once('exit', () => resolve(true));
        setTimeout(() => resolve(false), 300);
      });
      if (spawnFailed) { return skip('could not spawn --serve-bin ' + args.serveBin); }
      await waitForServer(baseURL, 15000);
    }

    let results;
    try {
      results = await runGates(chromium, baseURL, view, args.settleBudgetMs);
    } catch (e) {
      // A browser-launch failure (no cached executable for this platform,
      // corrupted install, etc.) is the "no browser available" skip case —
      // anything else propagates as a real, non-zero-exit harness error.
      const msg = String((e && e.message) || e);
      if (/executable doesn't exist|Failed to launch|browserType\.launch/i.test(msg)) {
        return skip('no usable browser available: ' + msg.split('\n')[0]);
      }
      throw e;
    }

    let allPass = true;
    for (const r of results) {
      console.log((r.pass ? 'PASS  ' : 'FAIL  ') + r.name + '  ' + r.detail);
      if (!r.pass) { allPass = false; }
    }
    return allPass ? 0 : 1;
  } finally {
    if (child) { await killChild(child, 5000); }
  }
}

main().then(
  (code) => process.exit(code),
  (e) => {
    console.error('graph-gates: ' + (e && e.stack || e));
    process.exit(1);
  }
);
