import { describe, it, expect } from 'vitest'
import { buildRedocSrcdoc } from '@/utils/redocHtml.ts'

describe('buildRedocSrcdoc', () => {
  it('returns empty string for empty spec', () => {
    expect(buildRedocSrcdoc('')).toBe('')
  })

  it('produces valid HTML with DOCTYPE', () => {
    const spec = '{"openapi":"3.0.0","info":{"title":"Test"},"paths":{}}'
    const result = buildRedocSrcdoc(spec)
    expect(result).toContain('<!DOCTYPE html>')
    expect(result).toContain('<html>')
    expect(result).toContain('</html>')
  })

  it('includes inlined ReDoc script (no external CDN)', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildRedocSrcdoc(spec)
    expect(result).not.toContain('<script src="https://cdn.redoc.ly')
    expect(result).toContain('Redoc.init')
  })

  it('embeds spec data in Redoc.init call', () => {
    const spec = '{"openapi":"3.0.0","info":{"title":"My API"}}'
    const result = buildRedocSrcdoc(spec)
    expect(result).toContain('Redoc.init(' + spec)
  })

  it('includes error handling try/catch', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildRedocSrcdoc(spec)
    expect(result).toContain('try {')
    expect(result).toContain('catch(e)')
    expect(result).toContain('Failed to render OpenAPI spec')
  })

  it('includes scrollbar styles with default colors', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildRedocSrcdoc(spec)
    expect(result).toContain('::-webkit-scrollbar')
    expect(result).toContain('#c1c1c1')
    expect(result).toContain('scrollbar-color')
  })

  it('uses custom scrollbar colors', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildRedocSrcdoc(spec, '#484f58', '#21262d')
    expect(result).toContain('#484f58')
    expect(result).toContain('#21262d')
  })
})
