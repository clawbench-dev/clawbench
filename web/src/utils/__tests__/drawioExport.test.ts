import { describe, expect, it, vi, afterEach } from 'vitest'
import {
  DRAWIO_EXPORT_FORMATS,
  exportPayload,
  exportedFileName,
  parseDataUri,
  saveExportToProject,
} from '@/utils/drawioExport'

describe('exportedFileName', () => {
  it('swaps the extension and keeps the base name', () => {
    expect(exportedFileName('flow.drawio', 'png')).toBe('flow.png')
    // Only the last extension goes: a dotted name keeps its stem.
    expect(exportedFileName('v1.2.flow.drawio', 'svg')).toBe('v1.2.flow.svg')
    expect(exportedFileName('', 'png')).toBe('diagram.png')
  })
})

describe('parseDataUri', () => {
  it('splits mime and payload', () => {
    expect(parseDataUri('data:image/png;base64,AAAA')).toEqual({ mime: 'image/png', base64: 'AAAA' })
  })

  it('rejects a plain string', () => {
    // XML comes back as text, not a data URI — callers must branch before this.
    expect(() => parseDataUri('<mxfile/>')).toThrow()
  })
})

describe('exportPayload', () => {
  it('keeps XML as text', () => {
    const { blob, base64 } = exportPayload('<mxfile/>')
    expect(base64).toBeNull()
    expect(blob.type).toBe('application/xml')
  })

  it('decodes an image data URI into real bytes', async () => {
    // "AQID" is 0x01 0x02 0x03.
    const { blob, base64 } = exportPayload('data:image/png;base64,AQID')
    expect(base64).toBe('AQID')
    expect(blob.type).toBe('image/png')
    expect(new Uint8Array(await blob.arrayBuffer())).toEqual(new Uint8Array([1, 2, 3]))
  })
})

describe('DRAWIO_EXPORT_FORMATS', () => {
  it('never offers PDF', () => {
    // The open-source editor answers a pdf request with an SVG, so offering PDF
    // would hand the user a .pdf file containing SVG text.
    expect(DRAWIO_EXPORT_FORMATS.map((f) => f.id)).toEqual(['png', 'svg', 'xml'])
  })
})

describe('saveExportToProject', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('posts the artifact to the upload endpoint with the target directory', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ path: 'docs/flow.png' }),
    })
    vi.stubGlobal('fetch', fetchMock)

    const saved = await saveExportToProject('data:image/png;base64,AQID', 'flow.png', '/proj/docs')
    expect(saved).toBe('docs/flow.png')

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/upload/file')
    const form = init.body as FormData
    expect(form.get('dir')).toBe('/proj/docs')
    expect((form.get('file') as File).name).toBe('flow.png')
  })

  it('surfaces the server reason instead of a generic failure', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue({ ok: false, status: 400, json: async () => ({ error: 'DirectoryNotFound' }) })
    )
    await expect(saveExportToProject('<mxfile/>', 'flow.drawio', '/nope')).rejects.toThrow('DirectoryNotFound')
  })
})
