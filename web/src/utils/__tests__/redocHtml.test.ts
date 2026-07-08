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

  it('includes ReDoc CDN script', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildRedocSrcdoc(spec)
    expect(result).toContain('cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js')
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

  it('includes sandbox-compatible meta charset', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildRedocSrcdoc(spec)
    expect(result).toContain('charset="utf-8"')
  })

  it('uses light theme by default', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildRedocSrcdoc(spec)
    expect(result).toContain("primary: { main: '#1890ff' }")
    expect(result).toContain('background: #fff')
  })

  it('uses dark theme when isDark is true', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildRedocSrcdoc(spec, true)
    expect(result).toContain("primary: { main: '#409eff' }")
    expect(result).toContain('background: #1e1e2e')
    expect(result).toContain("backgroundColor: '#1e1e2e'")
    expect(result).toContain("backgroundColor: '#11111b'")
  })
})
