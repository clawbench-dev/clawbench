const { chromium } = require('playwright');
const fs = require('fs');
const PW = fs.readFileSync('/tmp/forgeprobe/config/config.yaml','utf8').match(/^password:\s*"?([^"\n]*)"?\s*$/m)[1];
const BASE='https://localhost:20099';

const probe = () => {
  const controls = document.querySelector('.panel-window-controls');
  if (!controls) return { controls: false };
  const cr = controls.getBoundingClientRect();
  const pts = [];
  for (const fy of [0.25, 0.5, 0.75]) for (const fx of [0.15, 0.5, 0.85]) pts.push([cr.left + cr.width * fx, cr.top + cr.height * fy]);
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
  const hdr = document.querySelector('.col-left .tab-panel:not([style*="display: none"]) > *:first-child');
  return { controls: true, firstRow: hdr ? `${hdr.tagName}.${hdr.className}` : null, hits: [...new Set(hits)] };
};

(async () => {
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 860 }, ignoreHTTPSErrors: true, deviceScaleFactor: 1 });
  const page = await ctx.newPage();
  await page.addInitScript(() => { localStorage.setItem('clawbench_welcome_dismissed', 'true'); });
  await page.route('**/api/upgrade/**', r => r.fulfill({ status: 200, contentType: 'application/json', body: '{"has_upgrade":false,"latest_version":""}' }));
  await page.goto(BASE + '/login'); await page.waitForTimeout(1200);
  const pw = page.locator('input[type="password"]').first(); await pw.waitFor(); await pw.fill(PW); await pw.press('Enter');
  await page.waitForTimeout(2000);
  await page.evaluate(async () => { await fetch('/api/project', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ path: '/home/xulongzhe/projects/clawbench' }) }); });
  await page.goto(BASE + '/'); await page.waitForTimeout(2500);

  const out = {};
  const tab = async (name) => {
    await page.locator(`.dock-btn[data-tab="${name}"]`).filter({ visible: true }).first().click();
    await page.waitForTimeout(1200);
  };

  // 1) git history -> commit detail
  await tab('history');
  const commit = page.locator('.commit-row, .git-commit-row, [class*="commit"]').filter({ visible: true }).first();
  if (await commit.count()) {
    await commit.click().catch(() => {});
    await page.waitForTimeout(1500);
  }
  out['history-detail'] = await page.evaluate(probe);
  await page.screenshot({ path: '/tmp/audit2_history.png' });

  // 2) settings -> agents -> agent detail
  await tab('settings');
  const agentsEntry = page.locator('button, a, div').filter({ hasText: /智能体|Agents/ }).filter({ visible: true }).first();
  if (await agentsEntry.count()) { await agentsEntry.click().catch(() => {}); await page.waitForTimeout(1200); }
  const agentRow = page.locator('[class*="settings-agents-index__row"], [class*="agent-row"]').filter({ visible: true }).first();
  if (await agentRow.count()) { await agentRow.click().catch(() => {}); await page.waitForTimeout(1200); }
  out['settings-detail'] = await page.evaluate(probe);
  await page.screenshot({ path: '/tmp/audit2_settings.png' });

  // 3) tasks: empty in a fresh DB
  await tab('tasks');
  out['tasks'] = await page.evaluate(probe);

  // 4) kb: open a doc if any
  await tab('kb');
  const kbDoc = page.locator('[class*="kb-doc"], [class*="kb-file"]').filter({ visible: true }).first();
  if (await kbDoc.count()) { await kbDoc.click().catch(() => {}); await page.waitForTimeout(1200); }
  out['kb'] = await page.evaluate(probe);
  await page.screenshot({ path: '/tmp/audit2_kb.png' });

  console.log(JSON.stringify(out, null, 1));
  await browser.close();
})();
