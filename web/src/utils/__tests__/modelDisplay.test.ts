import { describe, expect, it } from 'vitest'
import { formatModelIdForDisplay } from '@/utils/modelDisplay'

describe('formatModelIdForDisplay', () => {
  it('renders a provider/model pair as provider/model', () => {
    // DeepSeek Harness identifies a model by a JSON-stringified pair; that exact
    // string is what the agent requires on the wire, so it must be shown in a
    // readable form rather than as a JSON blob.
    expect(formatModelIdForDisplay('["deepseek-official","deepseek-v4-pro"]'))
      .toBe('deepseek-official/deepseek-v4-pro')
  })

  it('joins pairs with more than two parts', () => {
    expect(formatModelIdForDisplay('["a","b","c"]')).toBe('a/b/c')
  })

  it('leaves ordinary opaque ids untouched', () => {
    expect(formatModelIdForDisplay('claude-sonnet-4-6')).toBe('claude-sonnet-4-6')
    expect(formatModelIdForDisplay('deepseek/deepseek-v4-flash')).toBe('deepseek/deepseek-v4-flash')
  })

  it('leaves malformed JSON-looking ids untouched', () => {
    // Never let a display helper throw or blank out an id it cannot parse.
    expect(formatModelIdForDisplay('[not json')).toBe('[not json')
    expect(formatModelIdForDisplay('[]')).toBe('[]')
    expect(formatModelIdForDisplay('[""]')).toBe('[""]')
    expect(formatModelIdForDisplay('["a",1]')).toBe('["a",1]')
    expect(formatModelIdForDisplay('[1,2]')).toBe('[1,2]')
  })

  it('leaves empty input untouched', () => {
    expect(formatModelIdForDisplay('')).toBe('')
  })
})
