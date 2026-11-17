const { chromium } = require('playwright');
const fs = require('fs');
const PW = fs.readFileSync('/tmp/forgeprobe/config/config.yaml','utf8').match(/^password:\s*"?([^"\n]*)"?\s*$/m)[1];
const BASE='https://localhost:20099';
const binding={platform:'gitlab',host:'gitlab.com',owner:'acme',repo:'widgets',slug:'acme/widgets'};
const item={platform:'gitlab',host:'gitlab.com',owner:'acme',repo:'widgets',type:'pr',number:681,
 title:'fix: forge header layout',body:'Some body text.',state:'open',author:'taka',commentCount:0,
 url:'https://gitlab.com/acme/widgets/-/merge_requests/681',createdAt:'2026-11-17T09:00:00Z',
 updatedAt:'2026-11-17T10:00:00Z',slug:'acme/widgets'};
(async()=>{
 const browser=await chromium.launch();
 for (const W of [900, 663]) {
  const ctx=await browser.newContext({viewport:{width:W,height:800},ignoreHTTPSErrors:true,deviceScaleFactor:2});
  const page=await ctx.newPage();
  await page.addInitScript(()=>{ localStorage.setItem('clawbench_welcome_dismissed','true'); });
  await page.route('**/api/upgrade/**', r=>r.fulfill({status:200,contentType:'application/json',body:'{"has_upgrade":false,"latest_version":""}'}));
  await page.route('**/api/forge/**', route=>{
    const p=new URL(route.request().url()).pathname;
    if(p==='/api/forge/binding') return route.fulfill({json:{binding}});
    if(p==='/api/forge/unread') return route.fulfill({json:{count:0,items:[]}});
    if(p==='/api/forge/items') return route.fulfill({json:{items:[item],hasMore:false,nextPage:1,binding}});
    if(p==='/api/forge/item') return route.fulfill({json:{item,binding}});
    if(p==='/api/forge/comments') return route.fulfill({json:{comments:[],hasMore:false,nextPage:1}});
    if(p==='/api/forge/item-pipelines') return route.fulfill({json:{pipelines:[],hasMore:false,nextPage:1,binding}});
    return route.fulfill({json:{}});
  });
  await page.goto(BASE+'/login'); await page.waitForTimeout(1200);
  const pw=page.locator('input[type="password"]').first();
  await pw.waitFor({timeout:10000}); await pw.fill(PW); await pw.press('Enter');
  await page.waitForTimeout(2000);
  await page.evaluate(async()=>{await fetch('/api/project',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'/home/xulongzhe/projects/clawbench'})});});
  await page.goto(BASE+'/'); await page.waitForTimeout(2500);
  const forgeBtn = page.locator('.dock-btn[data-tab="forge"]').filter({visible:true}).first();
  await forgeBtn.click(); await page.waitForTimeout(1500);
  await page.screenshot({path:`/tmp/list_${W}.png`});
  await page.waitForTimeout(300);
  await page.screenshot({path:`/tmp/after_click_${W}.png`});
  const rows = await page.evaluate(()=>[...document.querySelectorAll('.forge-panel *')].filter(e=>e.children.length===0&&e.textContent&&e.textContent.includes('681')).map(e=>e.className));
  console.log('681 nodes', rows);
  await page.screenshot({path:`/tmp/detail_${W}.png`});
  const dump=await page.evaluate(()=>{
    const h=document.querySelector('.forge-detail-header');
    const a=document.querySelector('.forge-detail-actions');
    const btns=[...document.querySelectorAll('.forge-icon-btn')];
    const box=el=>{const r=el.getBoundingClientRect();return {x:+r.x.toFixed(1),y:+r.y.toFixed(1),w:+r.width.toFixed(1),h:+r.height.toFixed(1)};};
    return {header:h?box(h):null, actions:a?{...box(a),bg:getComputedStyle(a).backgroundColor}:null,
      btns:btns.map(b=>({...box(b),bg:getComputedStyle(b).backgroundColor, cls:b.className})),
      dp:getComputedStyle(document.documentElement).getPropertyValue('--dp'),
      zoom:getComputedStyle(document.documentElement).zoom,
      headerH:getComputedStyle(document.documentElement).getPropertyValue('--header-height')};
  });
  console.log(W, JSON.stringify(dump,null,1));
  await ctx.close();
 }
 await browser.close();
})();
