import { chromium } from 'playwright';
import { unlink } from 'fs/promises';
import { resizeImage } from './lib/resize.mjs';

const outDir = '/home/xulongzhe/projects/clawbench/docs/screenshots';
const variants = ['product_hero', 'product_hero.en'];

const browser = await chromium.launch({
  executablePath: '/usr/bin/google-chrome',
  headless: true,
  args: ['--no-sandbox', '--disable-gpu'],
});

for (const variant of variants) {
  const fileUrl = 'file://' + outDir + '/' + variant + '.html';
  const tmpPath = outDir + '/' + variant + '@2x.png';
  const finalPath = outDir + '/' + variant + '.png';

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
  await page.close();

  await resizeImage(tmpPath, finalPath, 1920, 1080);
  await unlink(tmpPath);
  console.log('rendered', finalPath);
}

await browser.close();
console.log('done');
