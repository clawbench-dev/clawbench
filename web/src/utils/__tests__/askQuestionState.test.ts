import { describe, expect, it, beforeEach } from 'vitest'
import {
  getAskState,
  patchAskState,
  clearAskState,
  clearAskStatesByPrefix,
  askCardKey,
  askSessionPrefix,
  hasAskStatesForPrefix,
  _resetAskStatesForTesting,
  _askStateCountForTesting,
} from '../askQuestionState.ts'

describe('askQuestionState', () => {
  beforeEach(() => {
    _resetAskStatesForTesting()
  })

  it('returns undefined for a card the user has not touched', () => {
    expect(getAskState('tool:abc')).toBeUndefined()
  })

  it('stores and reads a supplementary note', () => {
    patchAskState('tool:abc', { supplementary: 'my notes' })
    expect(getAskState('tool:abc')?.supplementary).toBe('my notes')
  })

  it('stores selections per question index', () => {
    patchAskState('tool:abc', { selected: { '0': ['Option A'], '1': ['B', 'C'] } })
    const st = getAskState('tool:abc')
    expect(st?.selected['0']).toEqual(['Option A'])
    expect(st?.selected['1']).toEqual(['B', 'C'])
  })

  it('merges patches instead of replacing the whole state', () => {
    patchAskState('tool:abc', { supplementary: 'note' })
    patchAskState('tool:abc', { submitted: true })
    const st = getAskState('tool:abc')
    // The earlier note must survive a later unrelated patch — the DOM
    // restoration path patches one field at a time.
    expect(st?.supplementary).toBe('note')
    expect(st?.submitted).toBe(true)
  })

  it('lets a later patch overwrite an earlier value for the same field', () => {
    patchAskState('tool:abc', { supplementary: 'first' })
    patchAskState('tool:abc', { supplementary: 'second' })
    expect(getAskState('tool:abc')?.supplementary).toBe('second')
  })

  it('keeps cards independent', () => {
    patchAskState('tool:a', { supplementary: 'A' })
    patchAskState('tool:b', { supplementary: 'B' })
    expect(getAskState('tool:a')?.supplementary).toBe('A')
    expect(getAskState('tool:b')?.supplementary).toBe('B')
  })

  it('drops the entry when a patch leaves it empty', () => {
    patchAskState('tool:abc', { supplementary: 'note' })
    expect(_askStateCountForTesting()).toBe(1)
    patchAskState('tool:abc', { supplementary: '' })
    expect(getAskState('tool:abc')).toBeUndefined()
    expect(_askStateCountForTesting()).toBe(0)
  })

  it('does not create an entry for an empty patch on an untouched card', () => {
    patchAskState('tool:never-touched', { supplementary: '' })
    expect(_askStateCountForTesting()).toBe(0)
  })

  it('treats an empty selection array as no selection', () => {
    patchAskState('tool:abc', { selected: { '0': [] } })
    expect(_askStateCountForTesting()).toBe(0)
    expect(getAskState('tool:abc')).toBeUndefined()
  })

  it('keeps an entry that has only a selection', () => {
    patchAskState('tool:abc', { selected: { '0': ['A'] } })
    expect(getAskState('tool:abc')?.selected['0']).toEqual(['A'])
  })

  it('keeps an entry that is only submitted', () => {
    patchAskState('tool:abc', { submitted: true })
    expect(getAskState('tool:abc')?.submitted).toBe(true)
    expect(_askStateCountForTesting()).toBe(1)
  })

  it('clearAskState drops only the named card', () => {
    patchAskState('tool:a', { supplementary: 'A' })
    patchAskState('tool:b', { supplementary: 'B' })
    clearAskState('tool:a')
    expect(getAskState('tool:a')).toBeUndefined()
    expect(getAskState('tool:b')?.supplementary).toBe('B')
  })

  it('clearAskStatesByPrefix drops matching cards only', () => {
    patchAskState('sess-1|tool:a', { supplementary: 'A' })
    patchAskState('sess-1|tool:b', { supplementary: 'B' })
    patchAskState('sess-2|tool:c', { supplementary: 'C' })
    clearAskStatesByPrefix('sess-1|')
    expect(getAskState('sess-1|tool:a')).toBeUndefined()
    expect(getAskState('sess-1|tool:b')).toBeUndefined()
    expect(getAskState('sess-2|tool:c')?.supplementary).toBe('C')
  })

  it('ignores empty keys rather than creating an unreachable entry', () => {
    patchAskState('', { supplementary: 'orphan' })
    expect(_askStateCountForTesting()).toBe(0)
    expect(getAskState('')).toBeUndefined()
    expect(() => clearAskState('')).not.toThrow()
    expect(() => clearAskStatesByPrefix('')).not.toThrow()
  })

  it('state lives at module scope, so it outlives any caller (remount)', () => {
    // The whole point of hoisting the store: a fresh component instance after a
    // list remount reads the same Map and can restore the card's DOM from it.
    patchAskState('tool:abc', { supplementary: 'survives remount', submitted: true })
    expect(getAskState('tool:abc')?.supplementary).toBe('survives remount')
    expect(getAskState('tool:abc')?.submitted).toBe(true)
  })

  describe('key construction', () => {
    it('scopes a card key by session so a session can be swept in bulk', () => {
      const key = askCardKey('sess-1', 'tool', 'tu-1')
      expect(key.startsWith(askSessionPrefix('sess-1'))).toBe(true)
    })

    it('keeps the same card in different sessions apart', () => {
      expect(askCardKey('sess-1', 'tool', 'tu-1')).not.toBe(askCardKey('sess-2', 'tool', 'tu-1'))
    })

    it('keeps the same id in different render kinds apart', () => {
      // A merged card and a single tool card hold different question sets, so
      // they must not share answer state.
      expect(askCardKey('s', 'tool', 'x')).not.toBe(askCardKey('s', 'msg', 'x'))
      expect(askCardKey('s', 'text', 'x')).not.toBe(askCardKey('s', 'summary', 'x'))
    })

    it('gives cards without a session a shared fallback scope', () => {
      expect(askCardKey('', 'tool', 'tu-1')).toBe('no-session|tool:tu-1')
      expect(askSessionPrefix('')).toBe('no-session|')
    })

    it('bulk cleanup via the session prefix clears exactly that session', () => {
      patchAskState(askCardKey('sess-1', 'tool', 'a'), { supplementary: 'A' })
      patchAskState(askCardKey('sess-1', 'msg', 'b'), { supplementary: 'B' })
      patchAskState(askCardKey('sess-2', 'tool', 'c'), { supplementary: 'C' })

      clearAskStatesByPrefix(askSessionPrefix('sess-1'))

      expect(getAskState(askCardKey('sess-1', 'tool', 'a'))).toBeUndefined()
      expect(getAskState(askCardKey('sess-1', 'msg', 'b'))).toBeUndefined()
      expect(getAskState(askCardKey('sess-2', 'tool', 'c'))?.supplementary).toBe('C')
    })

    it('hasAskStatesForPrefix reports whether a session has any card state', () => {
      expect(hasAskStatesForPrefix(askSessionPrefix('sess-1'))).toBe(false)
      patchAskState(askCardKey('sess-1', 'tool', 'a'), { supplementary: 'A' })
      expect(hasAskStatesForPrefix(askSessionPrefix('sess-1'))).toBe(true)
      expect(hasAskStatesForPrefix(askSessionPrefix('sess-2'))).toBe(false)
      expect(hasAskStatesForPrefix('')).toBe(false)
    })
  })
})
