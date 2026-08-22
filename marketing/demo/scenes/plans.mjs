import { title, click, type, glide, beat } from '../lib/overlay.mjs';

const MEMBER = process.env.DEMO_PLAN_MEMBER ?? 'monorepo';
const SLUG = process.env.DEMO_PLAN_SLUG ?? 'config-access-roles';

// Plans list for one member -> filter to a slug -> its spec -> pick another
// version from the rail -> the design doc -> the scratchpad bundle files.
export default {
  name: 'plans',
  async run(page, { base }) {
    await page.goto(`${base}/plans?member=${MEMBER}`);
    await page.waitForSelector('li.plans-row');
    await title(page, { kicker: '04', heading: 'Plans', sub: 'Every unit of work: its design, its spec, every version across worktrees, and the live scratchpad.' });

    await page.mouse.wheel(0, 500);
    await beat(page, 1400);
    await page.mouse.wheel(0, -500);
    await beat(page, 600);

    await type(page, page.locator('input.plans-filter-input'), SLUG.split('-')[0], { after: 1300 });
    const row = page.locator('li.plans-row', { has: page.locator('span.plans-row-slug', { hasText: SLUG }) }).first();
    await glide(page, row.locator('span.plans-row-dots'), { settle: 900 });
    await click(page, row.locator('span.plans-row-title'), { after: 800 });
    await page.waitForSelector('[data-route="plans-slug"] .page-body, [data-route="plans-slug"]');
    await beat(page, 2200);

    const rail = page.locator('#right-rail');
    const entry = (label) => rail.locator('.bnav div', { hasText: new RegExp(`^${label}$`) });
    for (const label of ['BRIEF.md', 'STATE.md', 'design.md']) {
      if (!(await entry(label).count())) continue;
      await click(page, entry(label), { after: 600 });
      await page.waitForSelector('[data-route="plans-slug"]');
      await beat(page, label === 'design.md' ? 2400 : 1700);
      if (label === 'design.md') {
        await page.mouse.wheel(0, 600);
        await beat(page, 1400);
      }
    }

    await click(page, entry('spec.md'), { after: 1200 });
    const picker = rail.locator('input.vpick-input');
    if (await picker.count()) {
      await click(page, picker, { after: 1000 });
      const opts = rail.locator('.vmenu .vopt');
      if ((await opts.count()) > 1) {
        await click(page, opts.filter({ hasNot: page.locator('.on') }).nth(1), { after: 2600 });
      } else {
        await page.keyboard.press('Escape');
      }
    }
    await beat(page, 800);
  },
};
