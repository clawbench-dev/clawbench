import { chromium } from 'playwright'

const browser = await chromium.launch({ args: ['--enable-unsafe-swiftshader', '--use-gl=angle'] })
const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
await page.goto('file:///home/xulongzhe/projects/clawbench/test/earth-bench/index.html')
await page.waitForTimeout(800)

// MSAA（antialias:true）与超采样是两种不同的抗锯齿手段，成本结构不同。
// 这里直接对比：同一场景下 MSAA 开关的代价。
const r = await page.evaluate(async () => {
  const W = 2160, H = 1350
  const src = document.createElement('canvas')
  src.width = W; src.height = H
  const s = src.getContext('2d')
  s.fillStyle = '#123'; s.fillRect(0, 0, W, H)
  // 画一些高对比斜边（模拟球体轮廓 / 纹理边缘）
  s.strokeStyle = '#fff'; s.lineWidth = 2
  for (let i = 0; i < 200; i++) {
    s.beginPath(); s.moveTo(i * 11, 0); s.lineTo(i * 11 + 600, H); s.stroke()
  }

  function bench (antialias) {
    const cv = document.createElement('canvas')
    cv.width = W; cv.height = H
    document.body.appendChild(cv)
    const gl = cv.getContext('webgl', { antialias, alpha: false })
    if (!gl) { cv.remove(); return null }
    const p = gl.createProgram()
    const mk = (t, src) => { const sh = gl.createShader(t); gl.shaderSource(sh, src); gl.compileShader(sh); return sh }
    gl.attachShader(p, mk(gl.VERTEX_SHADER, 'attribute vec2 a;varying vec2 v;void main(){v=a;gl_Position=vec4(a,0.,1.);}'))
    gl.attachShader(p, mk(gl.FRAGMENT_SHADER, 'precision highp float;varying vec2 v;uniform sampler2D t;void main(){gl_FragColor=texture2D(t,v*0.5+0.5);}'))
    gl.linkProgram(p); gl.useProgram(p)
    const b = gl.createBuffer(); gl.bindBuffer(gl.ARRAY_BUFFER, b)
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([-1,-1,3,-1,-1,3]), gl.STATIC_DRAW)
    const loc = gl.getAttribLocation(p, 'a')
    gl.enableVertexAttribArray(loc); gl.vertexAttribPointer(loc, 2, gl.FLOAT, false, 0, 0)
    const tex = gl.createTexture()
    gl.bindTexture(gl.TEXTURE_2D, tex)
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, src)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
    const px = new Uint8Array(4)
    // 预热
    for (let i = 0; i < 5; i++) { gl.drawArrays(gl.TRIANGLES, 0, 3); gl.readPixels(0,0,1,1,gl.RGBA,gl.UNSIGNED_BYTE,px) }
    const N = 30, out = []
    for (let i = 0; i < N; i++) {
      const t0 = performance.now()
      gl.drawArrays(gl.TRIANGLES, 0, 3)
      gl.readPixels(0, 0, 1, 1, gl.RGBA, gl.UNSIGNED_BYTE, px)
      out.push(performance.now() - t0)
    }
    out.sort((a, b) => a - b)
    const p50 = out[Math.floor(out.length / 2)]
    const samples = gl.getParameter(gl.SAMPLES)
    gl.getExtension('WEBGL_lose_context')?.loseContext()
    cv.remove()
    return { p50: +p50.toFixed(2), samples }
  }

  return { msaaOn: bench(true), msaaOff: bench(false) }
})

console.log('=== MSAA（antialias:true）成本，2160x1350 ===')
console.log(`  开启: p50 ${r.msaaOn.p50}ms   实际采样数 ${r.msaaOn.samples}x`)
console.log(`  关闭: p50 ${r.msaaOff.p50}ms   实际采样数 ${r.msaaOff.samples}x`)
const ratio = r.msaaOn.p50 / r.msaaOff.p50
console.log(`  MSAA 开销倍数: ${ratio.toFixed(2)}x`)
console.log(`  对比 1.5x 超采样（像素数 2.25x）—— 若 MSAA 更便宜则地球不必用超采样`)

await browser.close()
