import { title, click, type, glide, beat } from '../lib/overlay.mjs';

const MEMBER = process.env.DEMO_SCHEMA_MEMBER ?? 'monorepo';
const TABLE = process.env.DEMO_SCHEMA_TABLE ?? 'Milestone';

// Schema page for one member -> filter to a table -> open the table's source
// in the code modal -> open a "Written by" query -> open a "Read by" source.
export default {
  name: 'schema',
  async run(page, { base }) {
    await page.goto(`${base}/code/schema?member=${MEMBER}`);
    await page.waitForSelector('div.code-schema-table');
    await title(page, { kicker: '02', heading: 'SQL schema', sub: 'Tables, views, keys, and which code reads or writes each one. Built from the source, not a live database.' });

    await page.mouse.wheel(0, 900);
    await beat(page, 1600);
    await page.mouse.wheel(0, -900);
    await beat(page, 700);

    const filter = page.locator('input.code-schema-search-input');
    await type(page, filter, TABLE, { after: 1500 });

    const card = page.locator('div.code-schema-table', { has: page.locator('h4.code-schema-table-name button', { hasText: new RegExp(`^${TABLE}$`) }) }).first();
    await card.waitFor();
    await glide(page, card.locator('ul, .code-schema-columns').first(), { settle: 1200 });

    await click(page, card.locator('h4.code-schema-table-name button'), { after: 600 });
    await page.waitForSelector('#code-modal.open');
    await beat(page, 3200);
    await page.keyboard.press('Escape');
    await beat(page, 900);

    const writers = card.locator('div.code-schema-relation', { hasText: 'Written by' });
    if (await writers.count()) {
      await glide(page, writers.locator('.code-schema-relation-label'), { settle: 900 });
      await click(page, writers.locator('button.code-node-link').first(), { after: 600 });
      await page.waitForSelector('#code-modal.open');
      await beat(page, 3200);
      await page.keyboard.press('Escape');
      await beat(page, 900);
    }

    let readers = card.locator('div.code-schema-relation', { hasText: 'Read by' });
    if (!(await readers.count())) {
      const clear = page.locator('button.code-schema-search-clear');
      if (await clear.isVisible()) await click(page, clear, { after: 900 });
      readers = page.locator('div.code-schema-relation', { hasText: 'Read by' }).first();
    }
    if (await readers.count()) {
      await glide(page, readers.locator('.code-schema-relation-label').first(), { settle: 900 });
      await click(page, readers.locator('button.code-node-link').first(), { after: 600 });
      await page.waitForSelector('#code-modal.open');
      await beat(page, 3000);
      await page.keyboard.press('Escape');
      await beat(page, 1200);
    }
  },
};
