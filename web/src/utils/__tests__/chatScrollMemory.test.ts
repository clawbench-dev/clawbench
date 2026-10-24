import { describe, expect, it, beforeEach } from 'vitest'
import {
  saveChatScrollPosition,
  clearChatScrollPosition,
  getChatScrollPosition,
  hasChatScrollPosition,
  shouldRestoreChatScroll,
  _resetChatScrollMemory,
} from '../chatScrollMemory'

const entry = (scrollTop: number) => ({ scrollTop, anchorKey: '', anchorOffset: 0, ratio: 0 })

describe('chatScrollMemory', () => {
  beforeEach(() => {
    _resetChatScrollMemory()
  })

  it('reports an unknown session as absent', () => {
    expect(hasChatScrollPosition('s1')).toBe(false)
    expect(getChatScrollPosition('s1')).toBeUndefined()
  })

  it('stores the full entry per session', () => {
    const saved = { scrollTop: 500, anchorKey: 'db-12', anchorOffset: 40, ratio: 0.25 }
    saveChatScrollPosition('s1', saved)
    expect(hasChatScrollPosition('s1')).toBe(true)
    expect(getChatScrollPosition('s1')).toEqual(saved)
  })

  it('keeps sessions independent', () => {
    saveChatScrollPosition('s1', entry(100))
    saveChatScrollPosition('s2', entry(800))
    expect(getChatScrollPosition('s1')?.scrollTop).toBe(100)
    expect(getChatScrollPosition('s2')?.scrollTop).toBe(800)
  })

  it('overwrites a previous entry for the same session', () => {
    saveChatScrollPosition('s1', entry(100))
    saveChatScrollPosition('s1', entry(900))
    expect(getChatScrollPosition('s1')?.scrollTop).toBe(900)
  })

  it('clear forgets only the given session', () => {
    saveChatScrollPosition('s1', entry(100))
    saveChatScrollPosition('s2', entry(200))
    clearChatScrollPosition('s1')
    expect(hasChatScrollPosition('s1')).toBe(false)
    expect(hasChatScrollPosition('s2')).toBe(true)
  })

  it('ignores an empty session id', () => {
    saveChatScrollPosition('', entry(100))
    expect(hasChatScrollPosition('')).toBe(false)
  })

  it('evicts the oldest entry past the 100-entry cap', () => {
    for (let i = 0; i < 100; i++) saveChatScrollPosition(`s${i}`, entry(i))
    // At the cap: nothing evicted yet.
    expect(hasChatScrollPosition('s0')).toBe(true)
    saveChatScrollPosition('s100', entry(100))
    // Oldest (FIFO) evicted, the newcomer and the rest survive.
    expect(hasChatScrollPosition('s0')).toBe(false)
    expect(hasChatScrollPosition('s1')).toBe(true)
    expect(hasChatScrollPosition('s100')).toBe(true)
  })
})

describe('shouldRestoreChatScroll', () => {
  it('restores a still session that has a remembered position', () => {
    expect(shouldRestoreChatScroll(true, false)).toBe(true)
  })

  it('lands at the bottom when there is no remembered position', () => {
    expect(shouldRestoreChatScroll(false, false)).toBe(false)
  })

  it('lands at the bottom for a RUNNING session even with a position', () => {
    // Its output is the point — restoring mid-history would hide it.
    expect(shouldRestoreChatScroll(true, true)).toBe(false)
  })
})
