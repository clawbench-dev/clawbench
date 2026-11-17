const { chromium } = require('playwright');
const fs = require('fs');
const PW = fs.readFileSync('/tmp/forgeprobe/config/config.yaml','utf8').match(/^password:\s*"?([^"\n]*)"?\s*$/m)[1];
const BASE='https://localhost:20099';
const binding={platform:'gitlab',host:'gitlab.com',owner:'acme',repo:'widgets',slug:'acme/widgets'};
const item={platform:'gitlab',host:'gitlab.com',owner:'acme',repo:'widgets',type:'pr',number:681,
 title:'fix: forge header layout',body:'Some body text.',state:'open',author:'taka',commentCount:0,
 url:'https://gitlab.com/acme/widgets/-/merge_requests/681',createdAt:'2026-11-17T09:00:00Z',
 updatedAt:'2026-11-17T10:00:00Z',slug:'acme/widgets'};

const probe = () => {
  const controls = document.querySelector('.panel-window-controls');
  if (!controls) return { controls: false };
  const cr = controls.getBoundingClientRect();
  const pts = [
    [cr.left + 6, cr.top + cr.height / 2],
    [cr.left + cr.width / 2, cr.top + cr.height / 2],
    [cr.right - 6, cr.top + cr.height / 2],
    [cr.left + 6, cr.top + 4],
    [cr.left + 6, cr.bottom - 4],
  ];
  const hits = [];
  for (const [x, y] of pts) {
    for (const el of document.elementsFromPoint(x, y)) {
      if (el === controls || controls.contains(el) || el.classList.contains('panel-head')) continue;
      if (!el.closest('.col-left')) continue;
      if (!['BUTTON','A','INPUT','SELECT','TEXTAREA'].includes(el.tagName)) continue;
      const label = (el.getAttribute('title') || el.getAttribute('aria-label') || el.textContent || '').trim().slice(0, 24);
      hits.push(`${el.tagName}.${el.className || ''}[${label}]`);
    }
  }
  return { controls: true, rect: { x: Math.round(cr.x), w: Math.round(cr.width) }, hits: [...new Set(hits)] };
};

(async () => {
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 860 }, ignoreHTTPSErrors: true, deviceScaleFactor: 1 });
  const page = await ctx.newPage();
  await page.addInitScript(() => { localStorage.setItem('clawbench_welcome_dismissed', 'true'); });
  await page.route('**/api/upgrade/**', r => r.fulfill({ status: 200, contentType: 'application/json', body: '{"has_upgrade":false,"latest_version":""}' }));
  await page.route('**/api/forge/**', route => {
    const p = new URL(route.request().url()).pathname;
    if (p === '/api/forge/binding') return route.fulfill({ json: { binding } });
    if (p === '/api/forge/unread') return route.fulfill({ json: { count: 0, items: [] } });
    if (p === '/api/forge/unread-items') return route.fulfill({ json: { count: 1, items: [{ itemKey: 'pr/681', type: 'pr', number: 681, runId: 0, eventType: 'opened', eventCount: 1, url: item.url, slug: 'acme/widgets', updatedAt: item.updatedAt, read: false }] } });
    if (p === '/api/forge/items') return route.fulfill({ json: { items: [item], hasMore: false, nextPage: 1, binding } });
    if (p === '/api/forge/item') return route.fulfill({ json: { item, binding } });
    if (p === '/api/forge/comments') return route.fulfill({ json: { comments: [], hasMore: false, nextPage: 1 } });
    if (p === '/api/forge/item-pipelines') return route.fulfill({ json: { pipelines: [], hasMore: false, nextPage: 1, binding } });
    if (p === '/api/forge/read') return route.fulfill({ json: { count: 0 } });
    return route.fulfill({ json: {} });
  });
  await page.goto(BASE + '/login'); await page.waitForTimeout(1200);
  const pw = page.locator('input[type="password"]').first(); await pw.waitFor(); await pw.fill(PW); await pw.press('Enter');
  await page.waitForTimeout(2000);
  await page.evaluate(async () => { await fetch('/api/project', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path: '/home/xulongzhe/projects/clawbench' }) }); });
  await page.goto(BASE + '/'); await page.waitForTimeout(2500);

  const tabs = await page.evaluate(() => [...document.querySelectorAll('.dock-btn[data-tab]')]
    .filter(b => b.getBoundingClientRect().width > 0)
    .map(b => b.getAttribute('data-tab')));
  console.log('tabs:', tabs.join(', '));

  const results = {};
  for (const tab of tabs) {
    await page.locator(`.dock-btn[data-tab="${tab}"]`).filter({ visible: true }).first().click();
    await page.waitForTimeout(1200);
    results[tab] = await page.evaluate(probe);
    await page.screenshot({ path: `/tmp/audit_${tab}.png` });
  }

  // forge drill-down (the fixed case) + pipeline drill-down
  await page.locator('.dock-btn[data-tab="forge"]').filter({ visible: true }).first().click();
  await page.waitForTimeout(800);
  await page.evaluate(() => { window.dispatchEvent(new CustomEvent('clawbench-open-forge', { detail: { target: { type: 'pr', number: 681, runId: 0, itemKey: 'pr/681' } } })); });
  await page.waitForTimeout(1200);
  results['forge-detail'] = await page.evaluate(probe);
  await page.screenshot({ path: '/tmp/audit_forge_detail.png' });

  console.log(JSON.stringify(results, null, 1));
  await browser.close();
})();
