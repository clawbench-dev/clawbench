import { describe, expect, it } from 'vitest'
import { isSummaryModelConfigured } from '@/utils/aiSummaryModel'

describe('isSummaryModelConfigured', () => {
  it('is false when there is no server config at all', () => {
    expect(isSummaryModelConfigured(undefined)).toBe(false)
  })

  it('is false when the config has no ai_summary block', () => {
    expect(isSummaryModelConfigured({ chat: { recommend_enabled: true } })).toBe(false)
  })

  it('is false when ai_summary has no provider binding', () => {
    expect(isSummaryModelConfigured({ ai_summary: { model: 'gpt-4o-mini' } })).toBe(false)
  })

  it('is false when provider_id is 0 or missing', () => {
    expect(isSummaryModelConfigured({ ai_summary: { provider_id: 0 } })).toBe(false)
    expect(isSummaryModelConfigured({ ai_summary: { provider_id: null } })).toBe(false)
  })

  it('is true when a provider is bound', () => {
    expect(isSummaryModelConfigured({ ai_summary: { provider_id: 7 } })).toBe(true)
  })
})
