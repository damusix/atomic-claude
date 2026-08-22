import { title, click, type, glide, beat } from '../lib/overlay.mjs';

const QUERY = process.env.DEMO_SEARCH_QUERY ?? 'change';
const MEMBER = process.env.DEMO_SEARCH_MEMBER ?? 'monorepo';

// Command-K palette: one query across markdown, code symbols, and plans.
// A code hit opens the code modal: find-in-file, then the symbol's callers
// in the intel pane, one hop in, Back. Then "View all results" lands on the
// full search page.
export default {
  name: 'search',
  async run(page, { base }) {
    // The palette's plans tab is scoped by ?member= on the current URL; from
    // the bare realm root it searches the empty local scope.
    await page.goto(`${base}/page/${MEMBER}/docs/wiki/index.md?member=${MEMBER}`);
    await page.waitForSelector('#main-pane .page-body');
    await title(page, { kicker: 'Search', heading: 'One box: markdown, symbols, plans', sub: 'Command-K anywhere. Code hits open straight into the source.' });
    await beat(page, 500);

    const trigger = page.locator('button.search-trigger');
    await click(page, trigger, { after: 500 });
    const modal = page.locator('#search-modal.open');
    await modal.waitFor();
    await page.keyboard.type(QUERY, { delay: 70 });
    // Rows are Ark combobox options keyed md:/code:/plan: in data-value.
    const results = page.locator('.search-results');
    await results.locator('[role="option"][data-value^="md:"]').first().waitFor({ timeout: 10_000 });
    await beat(page, 1800);

    // Changing the source clears the query, so it is typed again each time.
    const toggle = name => modal.locator('.search-toggle button.toggle-btn', { hasText: new RegExp(`^${name}$`) });
    const retype = async () => {
      await click(page, modal.locator('input.search-modal-input'), { after: 150 });
      await page.keyboard.press('ControlOrMeta+a');
      await page.keyboard.type(QUERY, { delay: 60 });
    };
    await click(page, toggle('code'), { after: 300 });
    await retype();
    await results.locator('[role="option"][data-value^="code:"]').first().waitFor({ timeout: 10_000 });
    await beat(page, 1600);

    await click(page, toggle('plans'), { after: 300 });
    await retype();
    await results.locator('[role="option"][data-value^="plans:"]').first().waitFor({ timeout: 10_000 });
    await beat(page, 1600);

    await click(page, toggle('code'), { after: 300 });
    await retype();
    // A hit from the demo member: its index is the one prep.sh refreshes, so
    // the modal is guaranteed source to show.
    const hit = results.locator(`[role="option"][data-value^="code:${MEMBER}:"]`).first();
    await hit.waitFor({ timeout: 10_000 });
    await click(page, hit, { after: 500 });
    const code = page.locator('#code-modal.open');
    await code.waitFor();
    await beat(page, 1600);

    const find = code.locator('input.code-find-input');
    await type(page, find, 'return', { delay: 70, after: 700 });
    for (let i = 0; i < 3; i++) {
      await page.keyboard.press('Enter');
      await beat(page, 650);
    }
    await beat(page, 400);

    const intel = code.locator('#code-modal-intel');
    const callers = intel.locator('nav.code-node-nav button', { hasText: /^callers$/ });
    if (await callers.count()) {
      await click(page, callers, { after: 600 });
      const chip = intel.locator('button.code-edge-chip-link').first();
      if (await chip.waitFor({ timeout: 6000 }).then(() => true, () => false)) {
        await beat(page, 900);
        await click(page, chip, { after: 1600 });
        const back = code.locator('button.code-modal-intel-back');
        if (await back.isVisible()) await click(page, back, { after: 1100 });
      }
    }
    await click(page, code.locator('.code-modal-close'), { after: 700 });

    await click(page, trigger, { after: 400 });
    await modal.waitFor();
    await retype();
    await results.locator('[role="option"]').first().waitFor({ timeout: 10_000 });
    await beat(page, 500);
    await click(page, modal.locator('button.search-viewall'), { after: 800 });
    await page.waitForSelector('[data-route="search"]');
    await beat(page, 1400);
    const codeTab = page.locator('.search-page-tabs button.search-tab', { hasText: /^Code$/ });
    if (await codeTab.count()) await click(page, codeTab, { after: 1400 });
    await page.mouse.wheel(0, 500);
    await beat(page, 1000);
  },
};
