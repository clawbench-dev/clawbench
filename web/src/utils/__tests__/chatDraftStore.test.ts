import { describe, expect, it, beforeEach } from 'vitest'
import {
  setChatDraft,
  getChatDraft,
  hasChatDraft,
  deleteChatDraft,
  _resetChatDraftsForTesting,
} from '../chatDraftStore.ts'

describe('chatDraftStore', () => {
  beforeEach(() => {
    _resetChatDraftsForTesting()
  })

  it('stores and reads a draft per session', () => {
    setChatDraft('sess-1', 'hello')
    expect(getChatDraft('sess-1')).toBe('hello')
    expect(hasChatDraft('sess-1')).toBe(true)
  })

  it('keeps drafts independent across sessions', () => {
    setChatDraft('sess-A', 'text A')
    setChatDraft('sess-B', 'text B')
    expect(getChatDraft('sess-A')).toBe('text A')
    expect(getChatDraft('sess-B')).toBe('text B')
    deleteChatDraft('sess-A')
    expect(hasChatDraft('sess-A')).toBe(false)
    expect(getChatDraft('sess-B')).toBe('text B')
  })

  it('treats an empty string as no draft', () => {
    setChatDraft('sess-1', 'something')
    expect(hasChatDraft('sess-1')).toBe(true)
    setChatDraft('sess-1', '')
    expect(hasChatDraft('sess-1')).toBe(false)
    expect(getChatDraft('sess-1')).toBe('')
  })

  it('returns empty/null-ish for unknown or empty session ids', () => {
    expect(getChatDraft('nope')).toBe('')
    expect(hasChatDraft('nope')).toBe(false)
    expect(getChatDraft('')).toBe('')
    expect(hasChatDraft('')).toBe(false)
    // Writing with no session id is a no-op rather than a crash.
    setChatDraft('', 'ignored')
    expect(hasChatDraft('')).toBe(false)
  })

  it('deleteChatDraft is a no-op for an unknown session', () => {
    expect(() => deleteChatDraft('never-seen')).not.toThrow()
    expect(() => deleteChatDraft('')).not.toThrow()
  })

  it('state lives at module scope, so it outlives any caller (project-switch remount)', () => {
    // A new component instance gets a fresh setup() but shares this module's
    // Map — that is the whole point of hoisting the store out of the component.
    setChatDraft('sess-1', 'survives remount')
    // Simulate the remount by reading through a fresh module-level access
    // (the functions are stateless; only the Map carries the value).
    expect(getChatDraft('sess-1')).toBe('survives remount')
  })
})
