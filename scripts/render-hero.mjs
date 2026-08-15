import { chromium } from 'playwright';
import { resizeImage } from './lib/resize.mjs';

const outDir = '/home/xulongzhe/projects/clawbench/docs/screenshots';
const fileUrl = 'file://' + outDir + '/product_hero.en.html';
const tmpPath = outDir + '/product_hero.en@2x.png';
const finalPath = outDir + '/product_hero.en.png';

const browser = await chromium.launch({
  executablePath: '/usr/bin/google-chrome',
  headless: true,
  args: ['--no-sandbox', '--disable-gpu'],
});
const page = await browser.newPage({
  viewport: { width: 1920, height: 1080 },
  deviceScaleFactor: 2,
});
await page.goto(fileUrl, { waitUntil: 'networkidle' });
await page.waitForTimeout(1500);
await page.screenshot({
  path: tmpPath,
  clip: { x: 0, y: 0, width: 1920, height: 1080 },
});
await browser.close();

await resizeImage(tmpPath, finalPath, 1920, 1080);
console.log('done');
