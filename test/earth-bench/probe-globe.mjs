import { chromium } from 'playwright'
import { serve } from './serve.mjs'

const srv = await serve(new URL('.', import.meta.url).pathname.replace(/\/$/, ''))
const browser = await chromium.launch({ args: ['--enable-unsafe-swiftshader', '--use-gl=angle'] })
const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
const errs = []
page.on('pageerror', (e) => errs.push('pageerror: ' + e.message))
page.on('console', (m) => { if (m.type() === 'error' && !/404/.test(m.text())) errs.push('console: ' + m.text().slice(0, 160)) })

await page.goto(srv.url + '/index.html')
await page.evaluate(() => window.__globe.ready)
await page.waitForTimeout(1200)

console.log('=== 启动状态 ===')
console.log(JSON.stringify(await page.evaluate(() => window.__globe.info()), null, 2))
console.log('fps =', await page.evaluate(() => window.__globe.getFps()))

const ui = await page.evaluate(() => ({
  sliders: document.querySelectorAll('#scroll input[type=range]').length,
  checkboxes: document.querySelectorAll('#scroll input[type=checkbox]').length,
  segButtons: document.querySelectorAll('#scroll .seg button').length,
  groups: [...document.querySelectorAll('#scroll .grp h4')].map((h) => h.textContent),
}))
console.log('\n=== 参数面板 ===')
console.log(`滑块 ${ui.sliders} · 复选 ${ui.checkboxes} · 分段按钮 ${ui.segButtons}`)
console.log('分组:', ui.groups.join(' / '))

console.log('\n=== 拨动每个滑块到极值 ===')
const bad = await page.evaluate(() => {
  const fails = []
  for (const inp of [...document.querySelectorAll('#scroll input[type=range]')]) {
    const label = inp.closest('.row').querySelector('.lab span').textContent
    const lo = parseFloat(inp.min), hi = parseFloat(inp.max)
    for (const v of [lo, hi, (lo + hi) / 2]) {
      inp.value = String(v)
      inp.dispatchEvent(new Event('input', { bubbles: true }))
    }
    if (window.__globe.getGl().isContextLost()) fails.push(label + ' → 上下文丢失')
  }
  return fails
})
console.log(bad.length ? '  失败: ' + bad.join(', ') : '  ✅ 全部极值安全')

console.log('\n=== 贴图切换（真实成功/失败判定）===')
for (const set of ['4k', '2k', '4k']) {
  const r = await page.evaluate((s) => window.__globe.switchTex(s), set)
  console.log(`  → ${set}: ${r.ok ? `OK ${r.size[0]}×${r.size[1]}` : 'FAIL ' + r.error}`)
  if (r.ok) {
    await page.waitForTimeout(1500)
    console.log(`     fps=${await page.evaluate(() => window.__globe.getFps())}  贴图=${JSON.stringify(await page.evaluate(() => window.__globe.texInfo()))}`)
  }
}

console.log('\n=== 上下文丢失 → 自动恢复链 ===')
// 注意：不要手动调 restoreContext()。webglcontextlost 里 preventDefault 之后
// 浏览器会自行尝试恢复；此时再手动 restore 会与之冲突，反而把上下文打回丢失态。
await page.evaluate(() => window.__globe.killContext())
await page.waitForTimeout(400)
console.log('  丢弃后 glLost =', await page.evaluate(() => window.__globe.getGl().isContextLost()))
let recovered = false
for (let i = 0; i < 12; i++) {
  await page.waitForTimeout(500)
  const r = await page.evaluate(() => ({
    lost: window.__globe.getGl().isContextLost(),
    ev: window.__globe.events(),
  }))
  if (!r.lost) { recovered = true; console.log(`  自动恢复成功（约 ${(i + 1) * 0.5}s）events=${JSON.stringify(r.ev)}`); break }
}
if (!recovered) console.log('  ⚠ 12 次轮询内未自动恢复')
if (recovered) {
  await page.waitForTimeout(1500)
  const st = await page.evaluate(() => ({
    fps: window.__globe.getFps(),
    tex: window.__globe.texInfo(),
    err: window.__globe.getGl().getError(),
  }))
  console.log('  恢复后 fps=' + st.fps + '  贴图=' + JSON.stringify(st.tex) + '  glError=' + st.err)
}

if (errs.length) console.log('\n=== 运行时错误 ===\n' + errs.slice(0, 8).join('\n'))
else console.log('\n=== 无运行时错误 ===')

await browser.close()
await srv.close()
