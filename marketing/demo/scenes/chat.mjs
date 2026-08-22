import { title, click, type, beat } from '../lib/overlay.mjs';

const ROOM = 'atomic-demo';

// Bus page: open the demo room (the tapes join the same fixed name), wait for the two agents (spawned by the
// tapes alongside this scene) to start talking, greet each from the browser,
// then hold on the live transcript. Narrower viewport: the two terminals
// take the right 640px of the composed frame.
export default {
  name: 'chat',
  viewport: { width: 1280, height: 1080 },
  tapes: ['agent-a.tape', 'agent-b.tape'],
  room: ROOM,
  async run(page, { base }) {
    await page.goto(`${base}/bus`);
    await page.waitForSelector('.bus-new-room input');
    await title(page, { kicker: '03', heading: 'Message Bus', sub: 'Concurrent Claude sessions talking over named rooms. The browser is one more member.' });

    await type(page, page.locator('.bus-new-room input'), ROOM, { after: 500 });
    await click(page, page.locator('.bus-new-room button[type="submit"]'), { after: 800 });
    await page.waitForSelector('section.bus-room-view');

    await page.waitForSelector('.bus-transcript article.bus-msg', { timeout: 90_000 });
    await beat(page, 4000);

    const composer = page.locator('form.bus-composer textarea');
    for (const [suffix, text] of [['agent-a', 'hello from the browser'], ['agent-b', 'hello to you too']]) {
      const name = await memberEndingWith(page, suffix);
      if (!name) continue;
      await type(page, composer, `@${name} `, { delay: 60, after: 400 });
      await page.keyboard.type(text, { delay: 55 });
      await beat(page, 500);
      await page.keyboard.press('Enter');
      await beat(page, 7000);
    }

    await beat(page, 30_000);
  },
};

// Member names carry the realm prefix (noorm-agent-a), so the browser
// addresses whatever the roster shows rather than the tape's short name.
async function memberEndingWith(page, suffix, timeout = 45_000) {
  const t0 = Date.now();
  while (Date.now() - t0 < timeout) {
    const names = await page.locator('.bus-members span.bus-member').allTextContents();
    const hit = names.map(n => n.trim().replace(/[×\s].*$/, '')).find(n => n.endsWith(suffix));
    if (hit) return hit;
    await page.waitForTimeout(1000);
  }
  return null;
}
