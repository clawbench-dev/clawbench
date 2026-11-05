import { describe, expect, it } from 'vitest'
import { buildLiveMeetingQuote, liveTranscriptText, LIVE_QUOTE_MAX } from '@/utils/liveMeetingQuote'
import { resolveQuoteType } from '@/utils/quoteItem'

describe('liveTranscriptText', () => {
  it('prefixes each line with its speaker', () => {
    const got = liveTranscriptText([
      { speaker: '张三', text: '本季度聚焦稳定性' },
      { speaker: '李四', text: '同意' },
    ])
    expect(got).toBe('张三: 本季度聚焦稳定性\n李四: 同意')
  })

  it('skips segments with no text and lines with no speaker', () => {
    const got = liveTranscriptText([
      { speaker: '张三', text: '   ' },
      { speaker: '', text: '背景音' },
      { speaker: '李四', text: '继续' },
    ])
    expect(got).toBe('背景音\n李四: 继续')
  })

  it('clips a long meeting', () => {
    const got = liveTranscriptText([{ speaker: 'S', text: 'x'.repeat(LIVE_QUOTE_MAX + 10) }])
    expect(got.length).toBe(LIVE_QUOTE_MAX + 1) // + the ellipsis
    expect(got.endsWith('…')).toBe(true)
  })
})

describe('buildLiveMeetingQuote', () => {
  it('carries the token and the transcript so the card can jump back', () => {
    const q = buildLiveMeetingQuote({
      token: 'tok-live',
      title: '产品评审会',
      segments: [{ speaker: '张三', text: '决议：聚焦稳定性' }],
    })!

    expect(q.meetingToken).toBe('tok-live')
    expect(q.text).toBe('张三: 决议：聚焦稳定性')
    expect(q.language).toBe('meeting')
    expect(resolveQuoteType(q)).toBe('meeting')
    // The name is the label; there is no start time while it runs.
    expect(q.filePath).toContain('产品评审会')
  })

  it('is still a valid card when nothing has been transcribed yet', () => {
    // A silent meeting is still a meeting: the card references it by name and
    // token rather than refusing to exist.
    const q = buildLiveMeetingQuote({ token: 'tok-live', title: '周会', segments: [] })!
    expect(q.text).toBe('')
    expect(q.meetingToken).toBe('tok-live')
  })

  it('refuses to build without a token', () => {
    expect(buildLiveMeetingQuote({ token: '', title: '周会', segments: [] })).toBeNull()
  })
})
