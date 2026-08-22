// Injected into every recorded page. Playwright's video never draws the OS
// pointer, so the cursor is a DOM element that follows the real mousemove
// events page.mouse.* fires. The title card is a DOM overlay for the same
// reason: keeping it inside the page means every scene is one webm with no
// post-hoc drawtext pass.

const INIT = `
(() => {
  const go = () => {
  if (window.__demo) return;
  const cur = document.createElement('div');
  cur.id = '__demo-cursor';
  Object.assign(cur.style, {
    position: 'fixed', left: '0', top: '0', width: '22px', height: '22px',
    zIndex: '2147483646', pointerEvents: 'none',
    transform: 'translate(-4px,-2px)', transition: 'transform 60ms linear',
    background: 'url("data:image/svg+xml;utf8,' + encodeURIComponent(
      '<svg xmlns=\\'http://www.w3.org/2000/svg\\' viewBox=\\'0 0 24 24\\'>' +
      '<path d=\\'M5 3l14 8.5-6.2 1.4L16 20l-2.3 1-3.2-7.2L5 18z\\' fill=\\'white\\' stroke=\\'black\\' stroke-width=\\'1.6\\' stroke-linejoin=\\'round\\'/></svg>'
    ) + '") no-repeat',
  });
  document.documentElement.appendChild(cur);
  const ring = document.createElement('div');
  ring.id = '__demo-ring';
  Object.assign(ring.style, {
    position: 'fixed', left: '0', top: '0', width: '36px', height: '36px',
    zIndex: '2147483645', pointerEvents: 'none', borderRadius: '50%',
    border: '3px solid #f5c451', opacity: '0', transform: 'translate(-18px,-18px) scale(0.4)',
  });
  document.documentElement.appendChild(ring);
  let x = 0, y = 0;
  window.addEventListener('mousemove', e => {
    x = e.clientX; y = e.clientY;
    cur.style.transform = 'translate(' + (x - 4) + 'px,' + (y - 2) + 'px)';
  }, true);
  window.addEventListener('mousedown', () => {
    ring.style.transition = 'none';
    ring.style.opacity = '0.9';
    ring.style.transform = 'translate(' + (x - 18) + 'px,' + (y - 18) + 'px) scale(0.4)';
    requestAnimationFrame(() => {
      ring.style.transition = 'opacity 450ms ease-out, transform 450ms ease-out';
      ring.style.opacity = '0';
      ring.style.transform = 'translate(' + (x - 18) + 'px,' + (y - 18) + 'px) scale(1.6)';
    });
  }, true);

  const card = document.createElement('div');
  card.id = '__demo-title';
  Object.assign(card.style, {
    position: 'fixed', inset: '0', zIndex: '2147483647', display: 'flex',
    flexDirection: 'column', alignItems: 'center', justifyContent: 'center',
    background: 'rgba(12,12,14,0.92)', color: '#f3f0e8', opacity: '0',
    pointerEvents: 'none', transition: 'opacity 420ms ease',
    fontFamily: 'Inter, -apple-system, Helvetica, sans-serif',
  });
  card.innerHTML = '<div id="__demo-kicker" style="font-size:22px;letter-spacing:0.22em;text-transform:uppercase;opacity:0.6;margin-bottom:18px"></div>'
    + '<div id="__demo-h" style="font-size:72px;font-weight:650;letter-spacing:-0.02em"></div>'
    + '<div id="__demo-sub" style="font-size:28px;opacity:0.75;margin-top:22px;max-width:900px;text-align:center;line-height:1.35"></div>';
  document.documentElement.appendChild(card);
  window.__demo = {
    title(kicker, h, sub) {
      card.querySelector('#__demo-kicker').textContent = kicker || '';
      card.querySelector('#__demo-h').textContent = h || '';
      card.querySelector('#__demo-sub').textContent = sub || '';
      card.style.opacity = '1';
    },
    untitle() { card.style.opacity = '0'; },
  };
  };
  if (document.documentElement) go(); else document.addEventListener('DOMContentLoaded', go);
})();
`;

export async function install(page) {
  await page.addInitScript(INIT);
}

export async function title(page, { kicker = '', heading, sub = '', hold = 1500 }) {
  await page.evaluate(INIT);
  await page.evaluate(([k, h, s]) => window.__demo.title(k, h, s), [kicker, heading, sub]);
  await page.waitForTimeout(hold);
  await page.evaluate(() => window.__demo.untitle());
  await page.waitForTimeout(500);
}

// Human-paced pointer travel. Playwright's mouse.move with steps interpolates
// linearly; an ease curve reads as a hand, not a teleport.
export async function glide(page, locator, { steps = 24, settle = 220 } = {}) {
  await locator.scrollIntoViewIfNeeded();
  const box = await locator.boundingBox();
  if (!box) throw new Error('glide: target has no bounding box');
  const tx = box.x + box.width / 2, ty = box.y + box.height / 2;
  const from = page.__demoPos || { x: 40, y: 40 };
  for (let i = 1; i <= steps; i++) {
    const t = i / steps, e = 1 - Math.pow(1 - t, 3);
    await page.mouse.move(from.x + (tx - from.x) * e, from.y + (ty - from.y) * e);
    await page.waitForTimeout(12);
  }
  page.__demoPos = { x: tx, y: ty };
  await page.waitForTimeout(settle);
}

// Down and up in one go: a held press lets outside-pointerdown dismissers
// (the search palette) unmount the target before the click completes.
export async function click(page, locator, opts = {}) {
  await glide(page, locator, opts);
  const { x, y } = page.__demoPos;
  await page.mouse.click(x, y);
  await page.waitForTimeout(opts.after ?? 650);
}

export async function type(page, locator, text, { delay = 60, after = 700 } = {}) {
  await click(page, locator, { after: 250 });
  await page.keyboard.type(text, { delay });
  await page.waitForTimeout(after);
}

export const beat = (page, ms = 1000) => page.waitForTimeout(ms);
