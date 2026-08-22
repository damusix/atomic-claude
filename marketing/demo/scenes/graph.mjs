import { title, click, type, glide, beat } from '../lib/overlay.mjs';

const SETTLE_MS = Number(process.env.DEMO_SETTLE_MS ?? 60_000);
const MEMBER = process.env.DEMO_GRAPH_MEMBER ?? 'monorepo';

// Docs graph and code graph, both already laid out: the warm-up page runs
// the simulation once so the recorded page replays the cached layout with
// no motion. Then: shift-hover the hub for its preview, ctrl-click to pin
// and highlight its neighbourhood, plain click to open it, search, and the
// same on the code graph.
//
// Nodes are GPU-picked inside a canvas, so positions come from
// window.GraphCore.debugState() (container-relative screen coords) and the
// pointer is driven there by hand.
export default {
  name: 'graph',
  async warm(page, { base }) {
    await page.goto(`${base}/graph`);
    await page.waitForFunction(() => window.GraphCore, null, { timeout: 30_000 });
    await settle(page);
    await page.goto(`${base}/graph?view=code&member=${MEMBER}`);
    await page.waitForFunction(() => window.GraphCore, null, { timeout: 30_000 });
    await settle(page);
    await page.waitForTimeout(1500);
  },
  async run(page, { base }) {
    await page.goto(`${base}/graph`);
    await page.waitForFunction(() => window.GraphCore, null, { timeout: 30_000 });
    await settle(page);
    await title(page, { kicker: 'Network graph', heading: 'Every page and every symbol, one map', sub: 'Shift to highlight and drag, ctrl-click to pin, click to open.' });
    await beat(page, 1200);

    let node = await biggestNode(page);
    if (node) {
      await hoverNode(page, node);
      await beat(page, 2200);
      await page.keyboard.up('Shift');
      await beat(page, 400);

      await ctrlClick(page, node);
      await beat(page, 2400);

      await plainClick(page, node);
      const modal = await page.waitForSelector('#cy-page-modal-scrim.open', { timeout: 5000 }).catch(() => null);
      if (modal) {
        await beat(page, 2400);
        await page.keyboard.press('Escape');
        await beat(page, 500);
      }
      await page.keyboard.press('Escape');
      await beat(page, 400);
    }

    const search = page.locator('input.graph-search-input');
    await type(page, search, 'change', { after: 1800 });
    const clear = page.locator('button.graph-search-clear');
    if (await clear.isVisible()) await click(page, clear, { after: 500 });

    await click(page, page.locator('button[data-graph-view="code"]'), { after: 600 });
    const sel = page.locator('select#graph-member-select');
    if (await sel.waitFor({ timeout: 8000 }).then(() => true, () => false)) {
      await glide(page, sel, { settle: 400 });
      await sel.selectOption(MEMBER);
    }
    await settle(page);
    await beat(page, 1800);

    node = await biggestNode(page);
    if (node) {
      await hoverNode(page, node);
      await beat(page, 2200);
      await page.keyboard.up('Shift');
      await beat(page, 400);
      await ctrlClick(page, node);
      await beat(page, 2200);
      await plainClick(page, node);
      const modal = await page.waitForSelector('#code-modal.open', { timeout: 5000 }).catch(() => null);
      if (modal) {
        await beat(page, 2600);
        await page.keyboard.press('Escape');
      }
      await page.keyboard.press('Escape');
    }
    await beat(page, 1200);
  },
};

async function settle(page) {
  await page.waitForSelector('.system-graph-loading', { state: 'hidden', timeout: SETTLE_MS }).catch(() => {});
  await page.waitForFunction(() => window.GraphCore.simRunning() === false, null, { timeout: SETTLE_MS }).catch(() => {});
}

async function biggestNode(page) {
  return page.evaluate(() => {
    const c = document.querySelector('[data-system-graph],[data-code-graph]');
    if (!c || !window.GraphCore) return null;
    const r = c.getBoundingClientRect();
    const st = window.GraphCore.debugState();
    let best = null;
    for (const [id, n] of Object.entries(st.nodes ?? {})) {
      if (!n.screen) continue;
      const x = r.left + n.screen.x, y = r.top + n.screen.y;
      if (x < r.left + 120 || x > r.right - 120 || y < r.top + 120 || y > r.bottom - 120) continue;
      if (!best || (n.size ?? 0) > best.size) best = { id, x, y, size: n.size ?? 0 };
    }
    return best;
  });
}

async function moveTo(page, { x, y }) {
  const from = page.__demoPos ?? { x: 200, y: 200 };
  for (let i = 1; i <= 26; i++) {
    const t = i / 26, e = 1 - Math.pow(1 - t, 3);
    await page.mouse.move(from.x + (x - from.x) * e, from.y + (y - from.y) * e);
    await page.waitForTimeout(12);
  }
  page.__demoPos = { x, y };
}

async function hoverNode(page, node) {
  await moveTo(page, node);
  await page.keyboard.down('Shift');
  await page.waitForSelector('#cy-preview-card.open', { timeout: 5000 }).catch(() => {});
}

async function ctrlClick(page, node) {
  await moveTo(page, node);
  await page.keyboard.down('Control');
  await page.mouse.down();
  await page.waitForTimeout(60);
  await page.mouse.up();
  await page.keyboard.up('Control');
}

async function plainClick(page, node) {
  await moveTo(page, node);
  await page.mouse.down();
  await page.waitForTimeout(60);
  await page.mouse.up();
}
