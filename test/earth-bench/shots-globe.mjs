import { chromium } from 'playwright'
import { serve } from './serve.mjs'
const srv = await serve(new URL('.', import.meta.url).pathname.replace(/\/$/, ''))
const browser = await chromium.launch({ args: ['--enable-unsafe-swiftshader', '--use-gl=angle'] })

// 桌面
const page = await browser.newPage({ viewport: { width: 1400, height: 850 } })
await page.goto(srv.url + '/index.html')
await page.evaluate(() => window.__globe.ready)
await page.waitForTimeout(2500)
await page.screenshot({ path: 'test/earth-bench/globe-default.png' })
console.log('globe-default.png')

// 展示面板
await page.click('#toggle')
await page.waitForTimeout(600)
await page.screenshot({ path: 'test/earth-bench/globe-panel.png' })
console.log('globe-panel.png')

// 手机
const m = await browser.newPage({ viewport: { width: 390, height: 844 }, deviceScaleFactor: 3 })
await m.goto(srv.url + '/index.html')
await m.evaluate(() => window.__globe.ready)
await m.waitForTimeout(2500)
await m.screenshot({ path: 'test/earth-bench/globe-mobile.png' })
console.log('globe-mobile.png')
await browser.close(); await srv.close()
