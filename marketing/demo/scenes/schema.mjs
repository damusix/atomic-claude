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
    await title(page, { kicker: 'SQL schema', heading: 'Tables, keys, readers, writers', sub: 'Built from the SQL in the repo, not from a live database.' });
    await beat(page, 900);

    const filter = page.locator('input.code-schema-search-input');
    await type(page, filter, TABLE, { after: 1100 });

    const card = page.locator('div.code-schema-table', { has: page.locator('h4.code-schema-table-name button', { hasText: new RegExp(`^${TABLE}$`) }) }).first();
    await card.waitFor();
    await glide(page, card.locator('ul, .code-schema-columns').first(), { settle: 900 });

    await click(page, card.locator('h4.code-schema-table-name button'), { after: 400 });
    await page.waitForSelector('#code-modal.open');
    await beat(page, 2600);
    await page.keyboard.press('Escape');
    await beat(page, 600);

    const writers = card.locator('div.code-schema-relation', { hasText: 'Written by' });
    if (await writers.count()) {
      await glide(page, writers.locator('.code-schema-relation-label'), { settle: 700 });
      await click(page, writers.locator('button.code-node-link').first(), { after: 400 });
      await page.waitForSelector('#code-modal.open');
      await beat(page, 2600);
      await page.keyboard.press('Escape');
      await beat(page, 600);
    }

    let readers = card.locator('div.code-schema-relation', { hasText: 'Read by' });
    if (!(await readers.count())) {
      const clear = page.locator('button.code-schema-search-clear');
      if (await clear.isVisible()) await click(page, clear, { after: 700 });
      readers = page.locator('div.code-schema-relation', { hasText: 'Read by' }).first();
    }
    if (await readers.count()) {
      await glide(page, readers.locator('.code-schema-relation-label').first(), { settle: 700 });
      await click(page, readers.locator('button.code-node-link').first(), { after: 400 });
      await page.waitForSelector('#code-modal.open');
      await beat(page, 2400);
      await page.keyboard.press('Escape');
      await beat(page, 800);
    }
  },
};
