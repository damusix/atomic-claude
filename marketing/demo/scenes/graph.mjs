import { title, click, type, glide, beat } from '../lib/overlay.mjs';

const SETTLE_MS = Number(process.env.DEMO_SETTLE_MS ?? 40_000);
const MEMBER = process.env.DEMO_GRAPH_MEMBER ?? 'monorepo';

// Docs graph: settle, shift-hover the biggest node for its preview card,
// shift-drag it, search. Then the code graph for one member.
//
// Nodes are GPU-picked inside a canvas, so positions come from
// window.GraphCore.debugState() (container-relative screen coords) and the
// pointer is driven there by hand.
export default {
  name: 'graph',
  async run(page, { base }) {
    await page.goto(`${base}/graph`);
    await page.waitForFunction(() => window.GraphCore, null, { timeout: 30_000 });
    await title(page, { kicker: '05', heading: 'Network graph', sub: 'Every page and every symbol as one force-directed map.' });
    await settle(page);
    await beat(page, 2500);

    let node = await biggestNode(page);
    if (node) {
      await hoverNode(page, node);
      await beat(page, 2800);
      await page.keyboard.up('Shift');
      await beat(page, 600);

      await dragNode(page, node, { dx: 160, dy: -90 });
      await page.waitForFunction(() => window.GraphCore.simRunning() === false, null, { timeout: SETTLE_MS }).catch(() => {});
      await beat(page, 1500);
    }

    const search = page.locator('input.graph-search-input');
    await type(page, search, 'core', { after: 2200 });
    const clear = page.locator('button.graph-search-clear');
    if (await clear.isVisible()) await click(page, clear, { after: 800 });

    await click(page, page.locator('button[data-graph-view="code"]'), { after: 800 });
    const sel = page.locator('select#graph-member-select');
    if (await sel.waitFor({ timeout: 8000 }).then(() => true, () => false)) {
      await glide(page, sel, { settle: 500 });
      await sel.selectOption(MEMBER);
      await beat(page, 800);
    }
    await settle(page);
    await beat(page, 3000);

    node = await biggestNode(page);
    if (node) {
      await hoverNode(page, node);
      await beat(page, 2800);
      await page.keyboard.up('Shift');
    }
    await beat(page, 2000);
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
      if (x < r.left + 80 || x > r.right - 80 || y < r.top + 80 || y > r.bottom - 80) continue;
      if (!best || (n.size ?? 0) > best.size) best = { id, x, y, size: n.size ?? 0 };
    }
    return best;
  });
}

async function hoverNode(page, { x, y }) {
  const from = page.__demoPos ?? { x: 200, y: 200 };
  for (let i = 1; i <= 30; i++) {
    const t = i / 30, e = 1 - Math.pow(1 - t, 3);
    await page.mouse.move(from.x + (x - from.x) * e, from.y + (y - from.y) * e);
    await page.waitForTimeout(12);
  }
  page.__demoPos = { x, y };
  await page.keyboard.down('Shift');
  await page.waitForSelector('#cy-preview-card.open', { timeout: 5000 }).catch(() => {});
}

async function dragNode(page, { x, y }, { dx, dy }) {
  await page.mouse.move(x, y);
  await page.keyboard.down('Shift');
  await page.mouse.down();
  await page.mouse.move(x + dx, y + dy, { steps: 30 });
  await page.mouse.up();
  await page.keyboard.up('Shift');
  page.__demoPos = { x: x + dx, y: y + dy };
}
