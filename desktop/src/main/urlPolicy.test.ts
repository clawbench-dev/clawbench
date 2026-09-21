import { describe, it, expect } from 'vitest'
import { classifyUrl } from './urlPolicy'

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