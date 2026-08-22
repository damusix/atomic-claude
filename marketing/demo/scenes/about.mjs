import { title, click, glide, beat } from '../lib/overlay.mjs';

// Outro: the About panel (version, bus rooms, wiki and index health), then a
// closing card.
export default {
  name: 'about',
  async run(page, { base }) {
    await page.goto(`${base}/`);
    await page.waitForSelector('#main-pane .page-body');
    await beat(page, 400);

    await click(page, page.locator('button.icon-rail-btn[aria-label^="About this server"]'), { after: 500 });
    const about = page.locator('#about-modal.open');
    await about.waitFor();
    await beat(page, 1800);
    const health = about.locator('.about-health, p.about-health-clear').first();
    if (await health.count()) await glide(page, health, { settle: 1400 });
    const repo = about.locator('.about-links a').first();
    if (await repo.count()) await glide(page, repo, { settle: 1000 });
    await click(page, about.locator('.about-close'), { after: 600 });

    await title(page, { kicker: 'atomic serve', heading: 'atomic claude install', sub: 'github.com/damusix/atomic-claude', hold: 3200 });
  },
};
