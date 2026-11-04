import { describe, expect, it } from 'vitest'
import { buildMailDocument, hasRemoteImages, sanitizeMailHtml } from '../mailHtml'

describe('sanitizeMailHtml', () => {
  it('removes scripts and event handlers', () => {
    const clean = sanitizeMailHtml('<div onclick="steal()">hi</div><script>alert(1)</script>')
    expect(clean).not.toContain('<script')
    expect(clean).not.toContain('onclick')
    expect(clean).toContain('hi')
  })

  it('removes form controls that could impersonate the app', () => {
    const clean = sanitizeMailHtml('<form><input value="password"><button>ok</button></form><p>body</p>')
    expect(clean).not.toContain('<form')
    expect(clean).not.toContain('<input')
    expect(clean).not.toContain('<button')
    expect(clean).toContain('body')
  })

  it('keeps ordinary formatting and images', () => {
    const clean = sanitizeMailHtml('<p><b>bold</b> <img src="https://x.test/a.png"></p>')
    expect(clean).toContain('<b>bold</b>')
    expect(clean).toContain('img')
  })
})

describe('buildMailDocument', () => {
  it('returns an empty document for empty input', () => {
    expect(buildMailDocument('')).toBe('')
  })

  it('blocks remote images by default', () => {
    const doc = buildMailDocument('<img src="https://track.test/pixel.png">')
    const csp = doc.match(/Content-Security-Policy" content="([^"]+)"/)?.[1] ?? ''
    expect(csp).toContain("img-src 'self' data:")
    expect(csp).not.toContain('https:')
    // The image tag itself survives; only the CSP stops the request.
    expect(doc).toContain('track.test')
  })

  it('allows remote images when the user opts in', () => {
    const doc = buildMailDocument('<img src="https://track.test/pixel.png">', { allowRemoteImages: true })
    expect(doc).toContain('img-src')
    expect(doc).toContain('https:')
  })

  it('forces links to open in a new tab', () => {
    const doc = buildMailDocument('<a href="https://example.com">go</a>')
    expect(doc).toContain('target="_blank"')
    expect(doc).toContain('rel="noopener noreferrer"')
  })

  it('always carries a CSP meta and the base stylesheet', () => {
    const doc = buildMailDocument('<p>hi</p>')
    expect(doc).toContain('Content-Security-Policy')
    expect(doc).toContain("default-src 'none'")
    expect(doc).toContain('<style>')
  })
})

describe('hasRemoteImages', () => {
  it('detects absolute and protocol-relative images', () => {
    expect(hasRemoteImages('<img src="https://a/b.png">')).toBe(true)
    expect(hasRemoteImages("<img src='//a/b.png'>")).toBe(true)
    expect(hasRemoteImages('<img src="data:image/png;base64,xx">')).toBe(false)
    expect(hasRemoteImages('<p>no images</p>')).toBe(false)
  })
})
