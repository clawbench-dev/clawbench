import { describe, expect, it } from 'vitest'
import { getBackendDisplayName } from '@/utils/backendNames'

describe('getBackendDisplayName', () => {
  it('returns display names for known backends', () => {
    expect(getBackendDisplayName('claude')).toBe('Claude')
    expect(getBackendDisplayName('codebuddy')).toBe('Codebuddy')
    expect(getBackendDisplayName('opencode')).toBe('OpenCode')
    expect(getBackendDisplayName('deepseek')).toBe('CodeWhale')
    expect(getBackendDisplayName('vecli')).toBe('VeCLI')
  })

  it('falls back to the backend id for unknown backends', () => {
    expect(getBackendDisplayName('unknown-backend')).toBe('unknown-backend')
    expect(getBackendDisplayName('')).toBe('')
  })
})
