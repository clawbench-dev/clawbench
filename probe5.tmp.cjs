const { chromium } = require('playwright');
const fs = require('fs');
const PW = fs.readFileSync('/tmp/forgeprobe/config/config.yaml','utf8').match(/^password:\s*"?([^"\n]*)"?\s*$/m)[1];
const BASE='https://localhost:20099';
const binding={platform:'gitlab',host:'gitlab.com',owner:'acme',repo:'widgets',slug:'acme/widgets'};
const item={platform:'gitlab',host:'gitlab.com',owner:'acme',repo:'widgets',type:'pr',number:681,
 title:'fix: forge header layout',body:'Some body text.',state:'open',author:'taka',commentCount:2,
 url:'https://gitlab.com/acme/widgets/-/merge_requests/681',createdAt:'2026-11-17T09:00:00Z',
 updatedAt:'2026-11-17T10:00:00Z',slug:'acme/widgets'};
const comments={comments:[{id:1,author:'bob',body:'looks fine',createdAt:'2026-11-17T09:30:00Z',updatedAt:'2026-11-17T09:30:00Z'}],hasMore:false,nextPage:1};

(async()=>{
 const browser=await chromium.launch();
 for (const W of [1280]) {
  const ctx=await browser.newContext({viewport:{width:W,height:800},ignoreHTTPSErrors:true,deviceScaleFactor:2});
  const page=await ctx.newPage();
  await page.addInitScript(()=>{localStorage.setItem('clawbench_welcome_dismissed','true');});
  await page.route('**/api/upgrade/**', r=>r.fulfill({status:200,contentType:'application/json',body:'{"has_upgrade":false,"latest_version":""}'}));
  await page.route('**/api/forge/**', route=>{
    const p=new URL(route.request().url()).pathname;
    if(p==='/api/forge/binding') return route.fulfill({json:{binding}});
    if(p==='/api/forge/unread') return route.fulfill({json:{count:0,items:[]}});
    if(p==='/api/forge/unread-items') return route.fulfill({json:{count:1,items:[{itemKey:'pr/681',type:'pr',number:681,runId:0,eventType:'opened',eventCount:1,url:item.url,slug:'acme/widgets',updatedAt:item.updatedAt,read:false}]}});
    if(p==='/api/forge/items') return route.fulfill({json:{items:[item],hasMore:false,nextPage:1,binding}});
    if(p==='/api/forge/item') return route.fulfill({json:{item,binding}});
    if(p==='/api/forge/comments') return route.fulfill({json:comments});
    if(p==='/api/forge/item-pipelines') return route.fulfill({json:{pipelines:[],hasMore:false,nextPage:1,binding}});
    if(p==='/api/forge/read') return route.fulfill({json:{count:0}});
    return route.fulfill({json:{}});
  });
  await page.goto(BASE+'/login'); await page.waitForTimeout(1200);
  const pw=page.locator('input[type="password"]').first(); await pw.waitFor(); await pw.fill(PW); await pw.press('Enter');
  await page.waitForTimeout(2000);
  await page.evaluate(async()=>{await fetch('/api/project',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({path:'/home/xulongzhe/projects/clawbench'})});});
  await page.goto(BASE+'/'); await page.waitForTimeout(2500);
  await page.locator('.dock-btn[data-tab="forge"]').filter({visible:true}).first().click();
  await page.waitForTimeout(1000);
  await page.evaluate(()=>{window.dispatchEvent(new CustomEvent('clawbench-open-forge',{detail:{target:{type:'pr',number:681,runId:0,itemKey:'pr/681'}}}));});
  await page.waitForTimeout(1500);
  await page.screenshot({path:`/tmp/detail_${W}.png`});
  const dump=await page.evaluate(()=>{
    const q=document.querySelector('.forge-icon-btn'); const r=q.getBoundingClientRect();
    const top=document.elementFromPoint(r.x+r.width/2, r.y+r.height/2);
    const stack=[]; let el=top; while(el && stack.length<6){stack.push(el.tagName+'.'+(el.className&&el.className.baseVal!==undefined?el.className.baseVal:el.className)); el=el.parentElement;}
    window.__stack=stack;
    window.__rects=[...document.querySelectorAll('*')].filter(e=>{const b=e.getBoundingClientRect(); return b.width>0&&b.height>0&&b.left<r.right&&b.right>r.left&&b.top<r.bottom&&b.bottom>r.top;}).map(e=>{const b=e.getBoundingClientRect();return {tag:e.tagName,cls:(e.className&&e.className.baseVal!==undefined?e.className.baseVal:e.className)||'',x:+b.x.toFixed(1),y:+b.y.toFixed(1),w:+b.width.toFixed(1),h:+b.height.toFixed(1),z:getComputedStyle(e).zIndex,pos:getComputedStyle(e).position};}).slice(0,20);
    const box=el=>{const r=el.getBoundingClientRect();return {x:+r.x.toFixed(1),y:+r.y.toFixed(1),w:+r.width.toFixed(1),h:+r.height.toFixed(1)};};
    const h=document.querySelector('.forge-detail-header');
    const a=document.querySelector('.forge-detail-actions');
    const btns=[...document.querySelectorAll('.forge-icon-btn')];
    return {header:h?box(h):null, actions:a?{...box(a),bg:getComputedStyle(a).backgroundColor}:null,
      btns:btns.map(b=>({...box(b),bg:getComputedStyle(b).backgroundColor, tag:b.tagName})),
      svgs:[...document.querySelectorAll('.forge-icon-btn svg')].map(box),
      html: document.documentElement.className,
      panelBox: document.querySelector('.forge-panel')?box(document.querySelector('.forge-panel')):null};
  });
  const extra=await page.evaluate(()=>({stack:window.__stack, rects:window.__rects}));
  console.log(W, JSON.stringify({...dump, ...extra},null,1));
  await ctx.close();
 }
 await browser.close();
})();
