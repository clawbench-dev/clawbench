import { describe, it, expect } from 'vitest'
import { buildSwaggerSrcdoc } from '@/utils/redocHtml.ts'

describe('buildSwaggerSrcdoc', () => {
  it('returns empty string for empty spec', () => {
    expect(buildSwaggerSrcdoc('')).toBe('')
  })

  it('produces valid HTML with DOCTYPE', () => {
    const spec = '{"openapi":"3.0.0","info":{"title":"Test"},"paths":{}}'
    const result = buildSwaggerSrcdoc(spec)
    expect(result).toContain('<!DOCTYPE html>')
    expect(result).toContain('<html>')
    expect(result).toContain('</html>')
  })

  it('includes Swagger UI CDN scripts', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec)
    expect(result).toContain('unpkg.com/swagger-ui-dist@5/swagger-ui.css')
    expect(result).toContain('unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js')
  })

  it('embeds spec data in SwaggerUIBundle call', () => {
    const spec = '{"openapi":"3.0.0","info":{"title":"My API"}}'
    const result = buildSwaggerSrcdoc(spec)
    expect(result).toContain('spec: ' + spec)
  })

  it('includes error handling try/catch', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec)
    expect(result).toContain('try {')
    expect(result).toContain('catch(e)')
    expect(result).toContain('Failed to render OpenAPI spec')
  })

  it('includes sandbox-compatible meta charset', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec)
    expect(result).toContain('charset="utf-8"')
  })

  it('uses classic theme by default (light)', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec)
    expect(result).toContain('"classic"')
    expect(result).toContain('background: #fff')
  })

  it('uses dark theme when isDark is true', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec, true)
    expect(result).toContain('"dark"')
    expect(result).toContain('background: #1a1a2e')
    expect(result).toContain('agate')
  })
})
