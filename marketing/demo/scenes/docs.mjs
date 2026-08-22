import { title, click, type, glide, beat } from '../lib/overlay.mjs';

// Realm index -> a member wiki page -> rail Links tab (filter, kind chip,
// follow an outbound link) -> rail Graph tab -> back to Overview.
export default {
  name: 'docs',
  async run(page, { base }) {
    await page.goto(`${base}/`);
    await page.waitForSelector('#main-pane .page-body');
    await title(page, { kicker: '01', heading: 'Wiki docs', sub: 'Every markdown file in the realm, rendered, linked, and cross-referenced.' });

    const member = page.locator('div.page-body a[href^="/page/"]', { hasText: 'monorepo' }).first();
    await click(page, member, { after: 1200 });
    await page.waitForSelector('#main-pane .page-body');
    await beat(page, 1200);

    await page.mouse.wheel(0, 700);
    await beat(page, 1600);
    await page.mouse.wheel(0, -700);
    await beat(page, 800);

    const rail = page.locator('#right-rail');
    await click(page, rail.getByRole('tab', { name: /^Links/ }), { after: 1300 });

    const filter = rail.locator('input.rail-search-input');
    await type(page, filter, 'core', { after: 1600 });
    const clear = rail.locator('button.rail-search-clear');
    if (await clear.isVisible()) await click(page, clear, { after: 900 });

    const chips = rail.locator('button.rail-kind');
    const md = chips.filter({ hasText: /^md/ }).first();
    if (await md.count()) {
      await click(page, md, { after: 1400 });
      await click(page, chips.filter({ hasText: /^all$/ }).first(), { after: 900 });
    }

    await click(page, rail.getByRole('tab', { name: 'Graph' }), { after: 3400 });
    await click(page, rail.getByRole('tab', { name: /^Links/ }), { after: 1000 });

    const outbound = rail.locator('#rail-out-content a.rail-edge.wikilink[href$=".md"]').first();
    await glide(page, outbound, { settle: 700 });
    await click(page, outbound, { after: 1400 });
    await page.waitForSelector('#main-pane .page-body');
    await beat(page, 1600);
    await page.mouse.wheel(0, 500);
    await beat(page, 1400);
    await click(page, rail.getByRole('tab', { name: 'Overview' }), { after: 1800 });
  },
};
