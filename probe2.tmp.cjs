const { chromium } = require('playwright');
const fs = require('fs');
const pw2 = fs.readFileSync('/tmp/forgeprobe/config/config.yaml','utf8').match(/^password:\s*"?([^"\n]*)"?\s*$/m)[1];
(async () => {
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 700, height: 800 }, ignoreHTTPSErrors: true });
  const page = await ctx.newPage();
  await page.goto('https://localhost:20099/login');
  await page.waitForTimeout(1500);
  const pwin = page.locator('input[type="password"]').first();
  await pwin.waitFor({ timeout: 10000 });
  await pwin.fill(pw2); await pwin.press('Enter');
  await page.waitForTimeout(2500);
  await page.evaluate(async () => {
    const r = await fetch('/api/project', { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify({ path: '/home/xulongzhe/projects/clawbench' }) });
    window.__proj = r.status;
  });
  await page.goto('https://localhost:20099/');
  await page.waitForTimeout(3000);
  await page.screenshot({ path: '/tmp/probe_state.png' });
  const vis = await page.evaluate(() => {
    const out = [];
    document.querySelectorAll('.dock-btn').forEach(b => {
      const r = b.getBoundingClientRect();
      out.push({tab: b.getAttribute('data-tab'), x: r.x, y: r.y, w: r.width, h: r.height, vis: r.width>0});
    });
    return { btns: out, bodyClass: document.body.className, htmlClass: document.documentElement.className };
  });
  console.log(JSON.stringify(vis, null, 1));
  await browser.close();
})();
