const { chromium } = require('playwright');
const fs = require('fs');
const yaml = (() => {
  const txt = fs.readFileSync('/tmp/forgeprobe/config/config.yaml', 'utf8');
  const m = txt.match(/^password:\s*"?([^"\n]*)"?\s*$/m);
  return m ? m[1] : '';
})();

const BASE = 'https://localhost:20099';
const REPO = '/home/xulongzhe/projects/clawbench';

const binding = { platform: 'gitlab', host: 'gitlab.com', owner: 'acme', repo: 'widgets', slug: 'acme/widgets' };
const item = {
  platform: 'gitlab', host: 'gitlab.com', owner: 'acme', repo: 'widgets',
  type: 'pr', number: 681, title: 'fix: forge header layout', body: 'Some body text.',
  state: 'open', author: 'taka', commentCount: 2, url: 'https://gitlab.com/acme/widgets/-/merge_requests/681',
  createdAt: '2026-11-17T09:00:00Z', updatedAt: '2026-11-17T10:00:00Z', slug: 'acme/widgets',
};

(async () => {
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 700, height: 800 }, ignoreHTTPSErrors: true, deviceScaleFactor: 1 });
  const page = await ctx.newPage();

  await page.route('**/api/forge/**', (route) => {
    const url = new URL(route.request().url());
    const path = url.pathname;
    if (path === '/api/forge/binding') return route.fulfill({ json: binding });
    if (path === '/api/forge/unread') return route.fulfill({ json: { count: 0, items: [] } });
    if (path === '/api/forge/items') return route.fulfill({ json: { items: [item], hasMore: false, nextPage: 1, binding } });
    if (path === '/api/forge/item') return route.fulfill({ json: { item, binding } });
    if (path === '/api/forge/comments') return route.fulfill({ json: { comments: [], hasMore: false, nextPage: 1 } });
    if (path === '/api/forge/item-pipelines') return route.fulfill({ json: { pipelines: [], hasMore: false, nextPage: 1, binding } });
    if (path === '/api/forge/pipelines') return route.fulfill({ json: { pipelines: [], hasMore: false, nextPage: 1, binding } });
    return route.fulfill({ json: {} });
  });

  await page.goto(BASE + '/login');
  await page.waitForTimeout(1500);
  const pw = page.locator('input[type="password"]').first();
  await pw.waitFor({ timeout: 10000 });
  await pw.fill(yaml);
  await pw.press('Enter');
  await page.waitForTimeout(2000);

  // set project cookie via API
  await page.evaluate(async () => {
    await fetch('/api/project', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path: '/home/xulongzhe/projects/clawbench' }) });
  });
  await page.waitForTimeout(1000);

  // open forge dock tab
  await page.goto(BASE + '/');
  await page.waitForTimeout(2000);
  const forgeBtn = page.locator('.dock-btn[data-tab="forge"]').first();
  if (await forgeBtn.count() === 0) {
    console.log('NO FORGE DOCK BUTTON; dumping dock buttons:');
    const btns = await page.locator('.dock-btn').all();
    for (const b of btns) console.log(await b.getAttribute('data-tab'));
  } else {
    await forgeBtn.click();
    await page.waitForTimeout(1500);
    const row = page.locator('.forge-item-row, .forge-list-row, [class*="forge"] button').filter({ hasText: '681' }).first();
    // generic: click the first element containing #681
    const any = page.locator('text=#681').first();
    await any.click({ timeout: 10000 });
    await page.waitForTimeout(1500);
    await page.screenshot({ path: '/tmp/forge_detail.png' });
    const header = page.locator('.forge-detail-header');
    console.log('header box:', JSON.stringify(await header.boundingBox()));
    const btns = await page.locator('.forge-icon-btn').all();
    for (const b of btns) console.log('icon-btn box:', JSON.stringify(await b.boundingBox()));
    const svg = page.locator('.forge-icon-btn svg').first();
    console.log('svg box:', JSON.stringify(await svg.boundingBox()));
    // dump actions container style
    const info = await page.evaluate(() => {
      const a = document.querySelector('.forge-detail-actions');
      if (!a) return 'no actions';
      const cs = getComputedStyle(a);
      return { w: a.getBoundingClientRect().width, gap: cs.gap, bg: cs.backgroundColor, pos: cs.position };
    });
    console.log('actions:', JSON.stringify(info));
  }
  await browser.close();
})();
