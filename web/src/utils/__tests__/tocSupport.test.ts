import { describe, expect, it } from 'vitest'
import { fileSupportsToc } from '@/utils/tocSupport'

describe('fileSupportsToc', () => {
  it('supports markdown with content', () => {
    expect(fileSupportsToc({ name: 'a.md', content: '# Hi' }, 'rendered')).toBe(true)
    expect(fileSupportsToc({ name: 'a.md', content: '# Hi' }, 'raw')).toBe(true)
  })

  it('supports PDF even without content (engine outline)', () => {
    expect(fileSupportsToc({ name: 'doc.pdf', content: null, isPdf: true })).toBe(true)
  })

  it('supports plain code/text files', () => {
    expect(fileSupportsToc({ name: 'main.ts', content: 'const x = 1' })).toBe(true)
    expect(fileSupportsToc({ name: 'data.json', content: '{"a":1}' })).toBe(true)
  })

  it('rejects office binaries (Excel/Word/PPT)', () => {
    expect(fileSupportsToc({ name: 'sheet.xlsx', content: null, isOffice: true })).toBe(false)
    expect(fileSupportsToc({ name: 'doc.docx', content: null, isOffice: true })).toBe(false)
    expect(fileSupportsToc({ name: 'deck.pptx', content: null, isOffice: true })).toBe(false)
  })

  it('rejects media / excalidraw', () => {
    expect(fileSupportsToc({ name: 'img.png', content: null, isImage: true })).toBe(false)
    expect(fileSupportsToc({ name: 'v.mp4', content: null, isVideo: true })).toBe(false)
    expect(fileSupportsToc({ name: 'a.mp3', content: null, isAudio: true })).toBe(false)
    expect(fileSupportsToc({ name: 'scene.excalidraw', content: '{}', isExcalidraw: true })).toBe(false)
  })

  it('rejects content-less files', () => {
    expect(fileSupportsToc({ name: 'f.txt', content: null })).toBe(false)
    expect(fileSupportsToc({ name: 'f.txt', content: '' })).toBe(false)
  })

  it('rejects HTML rendered preview but allows raw source', () => {
    const html = { name: 'page.html', content: '<html></html>', isHtml: true }
    expect(fileSupportsToc(html, 'rendered')).toBe(false)
    expect(fileSupportsToc(html, 'raw')).toBe(true)
  })

  it('rejects OpenAPI rendered preview but allows raw source', () => {
    const oas = { name: 'api.yaml', content: 'openapi: 3.0.0', subtype: 'openapi' }
    expect(fileSupportsToc(oas, 'rendered')).toBe(false)
    expect(fileSupportsToc(oas, 'raw')).toBe(true)
  })

  it('rejects the CSV table view but allows the source view', () => {
    const csv = { name: 'data.csv', content: 'a,b\n1,2' }
    expect(fileSupportsToc(csv, 'rendered')).toBe(false)
    expect(fileSupportsToc(csv, 'raw')).toBe(true)
    expect(fileSupportsToc({ name: 'data.tsv', content: 'a\tb' }, 'rendered')).toBe(false)
  })

  it('rejects null/undefined file', () => {
    expect(fileSupportsToc(null)).toBe(false)
    expect(fileSupportsToc(undefined)).toBe(false)
  })
})
