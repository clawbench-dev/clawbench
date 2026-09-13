import { describe, expect, it, beforeEach } from 'vitest'
import {
  injectChatInput,
  consumePendingChatInput,
  pendingChatInput,
  _resetChatInputInjectionForTesting,
} from '../chatInputInjection.ts'

describe('chatInputInjection', () => {
  beforeEach(() => {
    _resetChatInputInjectionForTesting()
  })

  it('queues text for later consumption', () => {
    injectChatInput('hello')
    expect(pendingChatInput.value).toBe('hello')
  })

  it('consume returns the queued text and clears it', () => {
    injectChatInput('hello')
    expect(consumePendingChatInput()).toBe('hello')
    expect(consumePendingChatInput()).toBeNull()
    expect(pendingChatInput.value).toBeNull()
  })

  it('ignores empty input', () => {
    injectChatInput('')
    expect(pendingChatInput.value).toBeNull()
  })

  it('later injections replace earlier ones (last wins)', () => {
    injectChatInput('first')
    injectChatInput('second')
    expect(consumePendingChatInput()).toBe('second')
  })
})
