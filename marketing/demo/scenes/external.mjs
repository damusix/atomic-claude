import { title, click, beat } from '../lib/overlay.mjs';

// External links registry: every outbound URL in the realm grouped by
// domain, each row naming the pages that cite it and when it first appeared.
export default {
  name: 'external',
  async run(page, { base }) {
    await page.goto(`${base}/external`);
    await page.waitForSelector('details.external-domain');
    await title(page, { kicker: 'External links', heading: 'Every outbound URL, by domain', sub: 'Which pages cite it, and when it first showed up.' });
    await beat(page, 900);

    const groups = page.locator('details.external-domain');
    await click(page, groups.nth(0).locator('summary'), { after: 1500 });
    await page.mouse.wheel(0, 500);
    await beat(page, 1200);
    await page.mouse.wheel(0, -500);
    await click(page, groups.nth(0).locator('summary'), { after: 500 });
    if ((await groups.count()) > 2) {
      await click(page, groups.nth(2).locator('summary'), { after: 1500 });
    }
    await beat(page, 600);
  },
};
