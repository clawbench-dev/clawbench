import { describe, it, expect } from 'vitest'
import { buildSwaggerSrcdoc } from '@/utils/swaggerHtml.ts'

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

  it('applies dark-mode class when isDark is true', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec, true)
    expect(result).toContain('class="dark-mode"')
  })

  it('does not apply dark-mode class when isDark is false', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec, false)
    expect(result).not.toContain('class="dark-mode"')
    expect(result).toContain('<html>')
  })

  it('includes inlined Swagger UI bundle script (no external CDN)', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec)
    expect(result).not.toContain('<script src="https://')
    expect(result).toContain('SwaggerUIBundle')
  })

  it('embeds spec data in SwaggerUIBundle config', () => {
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

  it('includes scrollbar styles with default colors', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec)
    expect(result).toContain('::-webkit-scrollbar')
    expect(result).toContain('#c1c1c1')
    expect(result).toContain('scrollbar-color')
  })

  it('uses custom scrollbar colors', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec, false, '#484f58', '#21262d')
    expect(result).toContain('#484f58')
    expect(result).toContain('#21262d')
  })

  it('hides the Swagger UI top bar', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec)
    expect(result).toContain('.swagger-ui .topbar { display: none; }')
  })

  it('overrides page margins to compact layout', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec)
    // Wrapper should use minimal horizontal padding and no max-width cap
    expect(result).toContain('.swagger-ui .wrapper { padding: 0 8px; max-width: none; }')
    // Operation blocks should use tighter margins/padding
    expect(result).toContain('.swagger-ui .opblock { margin: 0 0 10px; }')
    expect(result).toContain('.swagger-ui .opblock .opblock-summary { padding: 7px 8px; }')
  })

  it('includes inlined Swagger UI CSS', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec)
    // CSS is inlined via ?raw import, no <link> stylesheet tag
    expect(result).not.toContain('<link')
    expect(result).toContain('.swagger-ui')
  })

  it('includes requestInterceptor that routes through CORS proxy', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec)
    expect(result).toContain('requestInterceptor')
    expect(result).toContain('/api/openapi-proxy?url=')
    expect(result).toContain('encodeURIComponent')
  })

  it('requestInterceptor skips same-origin URLs', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec)
    expect(result).toContain('window.location.origin')
  })

  it('requestInterceptor strips Origin and Referer headers', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec)
    expect(result).toContain("delete req.headers['Origin']")
    expect(result).toContain("delete req.headers['Referer']")
  })

  it('requestInterceptor only proxies http/https URLs', () => {
    const spec = '{"openapi":"3.0.0"}'
    const result = buildSwaggerSrcdoc(spec)
    expect(result).toContain("req.url.startsWith('http://')")
    expect(result).toContain("req.url.startsWith('https://')")
  })
})
