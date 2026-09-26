/**
 * 起一个临时静态服务器。
 *
 * 为什么必须走 HTTP：`file://` 下加载的图片被当作跨源（opaque origin），
 * WebGL 的 texImage2D 会抛 SecurityError，Canvas2D 的 getImageData 也会被污染。
 * 这不影响真实部署（服务端本来就是 HTTP），但探针必须走 HTTP 才测得准。
 */
import { createServer } from 'node:http'
import { readFileSync, existsSync, statSync } from 'node:fs'
import { join, extname, normalize } from 'node:path'

const MIME = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.mjs': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.json': 'application/json',
  '.jpg': 'image/jpeg',
  '.jpeg': 'image/jpeg',
  '.png': 'image/png',
  '.webp': 'image/webp',
  '.svg': 'image/svg+xml',
  '.woff2': 'font/woff2',
}

export async function serve (root, port = 0) {
  const server = createServer((req, res) => {
    let p = decodeURIComponent(req.url.split('?')[0])
    if (p === '/') p = '/index.html'
    // 防路径穿越
    const safe = normalize(p).replace(/^(\.\.[/\\])+/, '')
    const f = join(root, safe)
    if (!f.startsWith(root) || !existsSync(f) || statSync(f).isDirectory()) {
      res.writeHead(404); res.end('not found'); return
    }
    res.writeHead(200, {
      'Content-Type': MIME[extname(f).toLowerCase()] || 'application/octet-stream',
      'Cache-Control': 'no-store',
    })
    res.end(readFileSync(f))
  })
  await new Promise((r) => server.listen(port, '127.0.0.1', r))
  const url = `http://127.0.0.1:${server.address().port}`
  return { url, close: () => new Promise((r) => server.close(r)) }
}
