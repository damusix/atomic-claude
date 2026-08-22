import { title, click, type, glide, beat } from '../lib/overlay.mjs';

const MEMBER = process.env.DEMO_DOCS_MEMBER ?? 'monorepo';

// Realm index -> Browse drawer (realm / repos / buckets tree) -> a member
// wiki page -> rail Links tab (filter, kind chip, Graph tab) -> follow an
// outbound link.
export default {
  name: 'docs',
  async run(page, { base }) {
    await page.goto(`${base}/`);
    await page.waitForSelector('#main-pane .page-body');
    await title(page, { kicker: 'Wiki docs', heading: 'Every markdown file, rendered and linked', sub: 'Realm wiki, member repos, and capture buckets in one tree.' });
    await beat(page, 600);

    await click(page, page.locator('button.icon-rail-btn[aria-label="Browse — toggle navigation"]'), { after: 900 });
    const drawer = page.locator('.nav-drawer[data-open]');
    await drawer.waitFor();
    const repo = drawer.locator('.nav-item.nav-folder', { hasText: new RegExp(`^${MEMBER}$`) }).first();
    if (await repo.count()) {
      await click(page, repo, { after: 800 });
      const docs = drawer.locator('.nav-item.nav-folder', { hasText: /^docs$/ }).first();
      if (await docs.count() && await docs.isVisible()) await click(page, docs, { after: 700 });
    }
    await page.mouse.wheel(0, 400);
    await beat(page, 900);
    const leaf = drawer.locator(`a.nav-item[href="/page/${MEMBER}/docs/wiki/index.md"]`).first();
    if (await leaf.count() && await leaf.isVisible()) {
      await click(page, leaf, { after: 700 });
    } else {
      await page.keyboard.press('Escape');
      await click(page, page.locator('div.page-body a[href^="/page/"]', { hasText: MEMBER }).first(), { after: 700 });
    }
    await page.waitForSelector('#main-pane .page-body');
    if (await drawer.isVisible()) {
      await page.keyboard.press('Escape');
      await beat(page, 500);
    }
    await beat(page, 1200);

    const rail = page.locator('#right-rail');
    await click(page, rail.getByRole('tab', { name: /^Links/ }), { after: 900 });

    const filter = rail.locator('input.rail-search-input');
    await type(page, filter, 'core', { after: 1100 });
    const clear = rail.locator('button.rail-search-clear');
    if (await clear.isVisible()) await click(page, clear, { after: 500 });

    const chips = rail.locator('button.rail-kind');
    const md = chips.filter({ hasText: /^md/ }).first();
    if (await md.count()) {
      await click(page, md, { after: 1000 });
      await click(page, chips.filter({ hasText: /^all$/ }).first(), { after: 500 });
    }

    await click(page, rail.getByRole('tab', { name: 'Graph' }), { after: 2400 });
    await click(page, rail.getByRole('tab', { name: /^Links/ }), { after: 600 });

    const outbound = rail.locator('#rail-out-content a.rail-edge.wikilink[href$=".md"]').first();
    await glide(page, outbound, { settle: 500 });
    await click(page, outbound, { after: 900 });
    await page.waitForSelector('#main-pane .page-body');
    await beat(page, 900);
    await page.mouse.wheel(0, 500);
    await beat(page, 900);
    await click(page, rail.getByRole('tab', { name: 'Overview' }), { after: 1200 });
  },
};
