// @vitest-environment jsdom
import { describe, expect, it, vi } from 'vitest'

// pdfjs-dist's legacy build unconditionally requires a native canvas module
// when it detects Node (we only parse, never render). Stub it so the test
// does not load @napi-rs/canvas's native bindings and leak a GC handle into
// the worker process.
vi.mock('@napi-rs/canvas', () => new Proxy({}, {
  get: () => { throw new Error('canvas not needed in this test') },
}))

// Regression test for the PDF preview on older engines (mobile WebViews,
// Chromium < 133).
// 旧引擎（手机 WebView、Chromium < 133）上 PDF 预览的回归测试。
// pdfjs-dist's modern build hard-requires
// Uint8Array.prototype.toHex (worker-side, used for PDF fingerprints),
// URL.parse and Promise.try. PdfPreview therefore imports the *legacy*
// builds, which bundle core-js polyfills. This test simulates an old engine
// by deleting those APIs before the legacy build is imported and verifies
// that (a) the build restores them and (b) a document still parses.

/** Build a structurally valid single-page PDF with a correct xref table. */
function buildMinimalPdf(): Uint8Array {
  const objects = [
    '<</Type/Catalog/Pages 2 0 R>>',
    '<</Type/Pages/Kids[3 0 R]/Count 1>>',
    '<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 200]>>',
  ]
  let pdf = '%PDF-1.4\n'
  const offsets: number[] = []
  objects.forEach((body, i) => {
    offsets.push(pdf.length)
    pdf += `${i + 1} 0 obj${body}endobj\n`
  })
  const startxref = pdf.length
  pdf += `xref\n0 ${objects.length + 1}\n0000000000 65535 f \n`
  for (const off of offsets) pdf += `${String(off).padStart(10, '0')} 00000 n \n`
  pdf += `trailer<</Size ${objects.length + 1}/Root 1 0 R>>\nstartxref\n${startxref}\n%%EOF`
  return new TextEncoder().encode(pdf)
}

describe('pdfjs legacy build on an old engine', () => {
  it('self-polyfills missing Uint8Array.prototype.toHex and URL.parse, then parses a PDF', async () => {
    const proto = Uint8Array.prototype as unknown as Record<string, unknown>
    const savedToHex = proto.toHex
    const savedParse = URL.parse
    delete proto.toHex
    ;(URL as unknown as Record<string, unknown>).parse = undefined
    expect(proto.toHex).toBeUndefined()
    expect(URL.parse).toBeUndefined()

    try {
      // Fresh module graph so core-js installs its polyfills now, with the
      // APIs missing — exactly like a first load on an old WebView.
      vi.resetModules()
      const pdfjs = await import('pdfjs-dist/legacy/build/pdf.mjs')

      expect(typeof proto.toHex).toBe('function')
      expect(typeof URL.parse).toBe('function')

      const doc = await pdfjs.getDocument({ data: buildMinimalPdf() }).promise
      expect(doc.numPages).toBe(1)
    } finally {
      if (savedToHex === undefined) delete proto.toHex
      else proto.toHex = savedToHex
      if (savedParse === undefined) delete (URL as any).parse
      else URL.parse = savedParse
    }
  })
})
