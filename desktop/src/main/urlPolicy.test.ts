import { describe, it, expect } from 'vitest'
import { classifyUrl, isAppPopup, shouldBlockPopupNavigation } from './urlPolicy'

describe('classifyUrl', () => {
  it('treats http links as external when outside the server origin', () => {
    expect(classifyUrl('https://example.com/page', 'https://clawbench.local')).toBe('external')
  })

  it('treats http links as internal when inside the server origin', () => {
    expect(classifyUrl('https://clawbench.local/settings', 'https://clawbench.local')).toBe('internal')
  })

  it('allows mailto links', () => {
    expect(classifyUrl('mailto:dev@example.com', 'https://clawbench.local')).toBe('external')
  })

  it('allows tel links', () => {
    expect(classifyUrl('tel:+15551234567', 'https://clawbench.local')).toBe('external')
  })

  it('treats http as external when no server is configured', () => {
    expect(classifyUrl('https://example.com/', undefined)).toBe('external')
  })

  it('blocks file: URLs', () => {
    expect(classifyUrl('file:///etc/passwd', 'https://clawbench.local')).toBe('block')
  })

  it('blocks javascript: URLs', () => {
    expect(classifyUrl('javascript:alert(1)', 'https://clawbench.local')).toBe('block')
  })

  it('blocks data: URLs', () => {
    expect(classifyUrl('data:text/html,<script>alert(1)</script>', 'https://clawbench.local')).toBe('block')
  })

  it('blocks unknown/custom schemes', () => {
    expect(classifyUrl('customapp://open/thing', 'https://clawbench.local')).toBe('block')
    expect(classifyUrl('vscode://file/foo', 'https://clawbench.local')).toBe('block')
  })

  it('blocks unparseable URLs', () => {
    expect(classifyUrl('not a url', 'https://clawbench.local')).toBe('block')
  })
})
describe('isAppPopup', () => {
  it('recognises the app\'s own about:blank popup', () => {
    // `window.open('')` — the member's screen in a window of its own.
    expect(isAppPopup('')).toBe(true)
    expect(isAppPopup('about:blank')).toBe(true)
  })

  it('does not exempt anything that could navigate somewhere', () => {
    expect(isAppPopup('about:blank#x')).toBe(false)
    expect(isAppPopup('https://example.com/')).toBe(false)
    expect(isAppPopup('file:///etc/passwd')).toBe(false)
    expect(isAppPopup('javascript:alert(1)')).toBe(false)
  })
})

describe('shouldBlockPopupNavigation', () => {
  it('keeps the popup on its blank document', () => {
    // The popup inherits this window's preload, so following a navigation would
    // hand the native bridge to whatever origin it lands on.
    expect(shouldBlockPopupNavigation('https://evil.example/')).toBe(true)
    expect(shouldBlockPopupNavigation('file:///etc/passwd')).toBe(true)
    expect(shouldBlockPopupNavigation('javascript:alert(1)')).toBe(true)
    expect(shouldBlockPopupNavigation('about:blank')).toBe(false)
    expect(shouldBlockPopupNavigation('')).toBe(false)
  })
})
