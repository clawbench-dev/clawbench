import { describe, expect, it } from 'vitest'
import { pickUnspokenReply } from '../speakableReply'

/**
 * Which reply still needs reading out.
 *
 * The interesting cases are the ones where saying nothing is correct: the
 * newest reply was already spoken, it has nothing speakable, or it has no id to
 * key on. Re-reading an older reply is worse than silence — the user has long
 * moved past it.
 */

const text = (t: string) => [{ type: 'text', text: t }]

describe('pickUnspokenReply', () => {
  it('returns the newest assistant reply when it has not been spoken', () => {
    const messages = [
      { role: 'user', id: 1, blocks: text('问题') },
      { role: 'assistant', id: 2, blocks: text('答案') },
    ]
    expect(pickUnspokenReply(messages, new Set())).toEqual({ id: '2', text: '答案' })
  })

  it('finds the reply when a user message was appended after it', () => {
    // A queued message lands after the still-finalizing reply, so "the last
    // message" is not the answer. Searching backwards is the point.
    const messages = [
      { role: 'assistant', id: 2, blocks: text('答案') },
      { role: 'user', id: 3, blocks: text('追加') },
    ]
    expect(pickUnspokenReply(messages, new Set())?.id).toBe('2')
  })

  it('says nothing when the newest reply was already spoken', () => {
    // Walking further back would replay an old answer the user moved past.
    const messages = [
      { role: 'assistant', id: 2, blocks: text('上一轮') },
      { role: 'user', id: 3, blocks: text('问题') },
      { role: 'assistant', id: 4, blocks: text('这一轮') },
    ]
    expect(pickUnspokenReply(messages, new Set(['4']))).toBeNull()
  })

  it('says nothing when the newest reply has no speakable text', () => {
    // A tool-only reply (an edit, a search) has nothing to read out, and the
    // previous answer must not be read in its place.
    const messages = [
      { role: 'assistant', id: 2, blocks: text('上一轮') },
      { role: 'assistant', id: 4, blocks: [{ type: 'tool_use', name: 'Edit', id: 't1' }] },
    ]
    expect(pickUnspokenReply(messages, new Set())).toBeNull()
  })

  it('falls back to the summary when the content was stripped', () => {
    // A summarized reply has its blocks replaced with an empty array; the
    // summary is the only text left in the history row.
    const messages = [{ role: 'assistant', id: 7, blocks: [], summary: '一段总结' }]
    expect(pickUnspokenReply(messages, new Set())).toEqual({ id: '7', text: '一段总结' })
  })

  it('ignores an assistant message without an id', () => {
    // Auto-speech keys by id, so such a bubble can neither be spoken nor be
    // remembered as spoken.
    const messages = [{ role: 'assistant', blocks: text('没有 id') }]
    expect(pickUnspokenReply(messages, new Set())).toBeNull()
  })

  it('reads the question card out of an AskUserQuestion reply', () => {
    const messages = [{
      role: 'assistant',
      id: 9,
      blocks: [{
        type: 'tool_use',
        name: 'AskUserQuestion',
        input: { questions: [{ question: '继续吗', options: [{ label: '好' }, { label: '算了' }] }] },
      }],
    }]
    expect(pickUnspokenReply(messages, new Set())?.text).toContain('继续吗')
  })

  it('returns null for an empty conversation', () => {
    expect(pickUnspokenReply([], new Set())).toBeNull()
  })
})
