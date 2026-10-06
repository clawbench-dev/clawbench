import { describe, expect, it } from 'vitest'
import { parseGroupRouting } from '@/utils/groupRouting.ts'

describe('parseGroupRouting before (pre-tag background)', () => {
  it('is empty when the tag leads the message', () => {
    const r = parseGroupRouting('<clawbench-speaker>A</clawbench-speaker> 请谈谈')
    expect(r.found).toBe(true)
    expect(r.before).toBe('')
  })

  it('returns the text preceding the tag', () => {
    const r = parseGroupRouting('A 的观点不错 <clawbench-speaker>B</clawbench-speaker> 请回应')
    expect(r.found).toBe(true)
    expect(r.before).toBe('A 的观点不错')
  })

  it('trims surrounding whitespace around the background', () => {
    const r = parseGroupRouting('\n  背景上下文 \n <clawbench-speaker>A</clawbench-speaker> 请表态')
    expect(r.before).toBe('背景上下文')
  })

  it('is empty when no tag is present', () => {
    const r = parseGroupRouting('普通发言，没有标签')
    expect(r.found).toBe(false)
    expect(r.before).toBe('')
  })

  it('still slices the background ahead of a malformed tag (never strips)', () => {
    const r = parseGroupRouting('背景在此 <clawbench-speaker></clawbench-speaker>')
    expect(r.found).toBe(false)
    expect(r.before).toBe('背景在此')
  })
})
