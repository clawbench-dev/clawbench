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
 const ctx=await browser.newContext({viewport:{width:900,height:800},ignoreHTTPSErrors:true,deviceScaleFactor:1});
 const page=await ctx.newPage();
 await page.addInitScript(()=>{localStorage.setItem('clawbench_welcome_dismissed','true');});
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
 const pw=page.locator('input[type="password"]').first(); await pw.waitFor(); await pw.fill(PW); await pw.press('Enter');
 await page.waitForTimeout(2000);
 await page.evaluate(async()=>{await fetch('/api/project',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'/home/xulongzhe/projects/clawbench'})});});
 await page.goto(BASE+'/'); await page.waitForTimeout(2500);
 await page.locator('.dock-btn[data-tab="forge"]').filter({visible:true}).first().click();
 await page.waitForTimeout(1200);
 const tab = page.locator('button').filter({hasText:'合并'}).first();
 await tab.click(); await page.waitForTimeout(1200);
 const html = await page.evaluate(()=>document.querySelector('.forge-panel')?.outerHTML.slice(0,4000));
 console.log(html.slice(3800));
 await page.screenshot({path:'/tmp/merge_list.png'});
 await browser.close();
})();
