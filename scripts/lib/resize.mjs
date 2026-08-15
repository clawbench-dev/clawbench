import { createRequire } from 'module';
const require = createRequire(import.meta.url);
const sharp = require('sharp');

export async function resizeImage(src, dest, width, height) {
  await sharp(src).resize(width, height, { kernel: 'lanczos3' }).toFile(dest);
}
