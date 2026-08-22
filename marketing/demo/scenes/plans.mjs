import { title, click, type, glide, beat } from '../lib/overlay.mjs';

const MEMBER = process.env.DEMO_PLAN_MEMBER ?? 'monorepo';
const SLUG = process.env.DEMO_PLAN_SLUG ?? 'config-access-roles';

// Plans list for one member -> filter to a slug -> its spec -> the scratchpad
// bundle files -> the design doc -> back to the spec and another version.
export default {
  name: 'plans',
  async run(page, { base }) {
    await page.goto(`${base}/plans?member=${MEMBER}`);
    await page.waitForSelector('li.plans-row');
    await title(page, { kicker: 'Plans', heading: 'Design, spec, every version, live scratchpad', sub: 'One row per unit of work, read across every worktree of the repo.' });
    await beat(page, 900);

    await type(page, page.locator('input.plans-filter-input'), SLUG.split('-')[0], { after: 900 });
    const row = page.locator('li.plans-row', { has: page.locator('span.plans-row-slug', { hasText: SLUG }) }).first();
    await glide(page, row.locator('span.plans-row-dots'), { settle: 700 });
    await click(page, row.locator('span.plans-row-title'), { after: 500 });
    await page.waitForSelector('[data-route="plans-slug"]');
    await beat(page, 1600);

    const rail = page.locator('#right-rail');
    const entry = (label) => rail.locator('.bnav div', { hasText: new RegExp(`^${label}$`) });
    for (const label of ['BRIEF.md', 'STATE.md', 'design.md']) {
      if (!(await entry(label).count())) continue;
      await click(page, entry(label), { after: 400 });
      await page.waitForSelector('[data-route="plans-slug"]');
      await beat(page, label === 'design.md' ? 1800 : 1200);
    }

    await click(page, entry('spec.md'), { after: 800 });
    const picker = rail.locator('input.vpick-input');
    if (await picker.count()) {
      await click(page, picker, { after: 700 });
      const opts = rail.locator('.vmenu .vopt');
      if ((await opts.count()) > 1) {
        await click(page, opts.filter({ hasNot: page.locator('.on') }).nth(1), { after: 1800 });
      } else {
        await page.keyboard.press('Escape');
      }
    }
    await beat(page, 500);
  },
};
