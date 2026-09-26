import { describe, expect, it } from 'vitest'
import { isSummaryModelConfigured } from '@/utils/aiSummaryModel'

describe('isSummaryModelConfigured', () => {
  it('is false when there is no server config at all', () => {
    expect(isSummaryModelConfigured(undefined)).toBe(false)
  })

  it('is false when the config has no ai_summary block', () => {
    expect(isSummaryModelConfigured({ chat: { recommend_enabled: true } })).toBe(false)
  })

  it('is false when ai_summary has no api block', () => {
    expect(isSummaryModelConfigured({ ai_summary: { model: 'gpt-4o-mini' } })).toBe(false)
  })

  it('is false when base_url is an empty string', () => {
    expect(isSummaryModelConfigured({ ai_summary: { api: { base_url: '' } } })).toBe(false)
  })

  it('is true when base_url is set', () => {
    expect(
      isSummaryModelConfigured({ ai_summary: { api: { base_url: 'https://summary.example.com' } } }),
    ).toBe(true)
  })
})
