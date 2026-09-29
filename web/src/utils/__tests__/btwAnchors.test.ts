import { describe, expect, it } from 'vitest'
import { btwAnchorKey, groupBtwRecords, messageAnchorKey, anchorCount, anchorPending, anchorKeyPending, currentAnchorKey } from '../btwAnchors'

describe('btwAnchorKey', () => {
  it('stringifies a numeric anchor id', () => {
    expect(btwAnchorKey(42)).toBe('42')
    expect(btwAnchorKey(0)).toBe('0')
  })

  it('treats null/undefined/NaN as the 0 anchor (asked before any message)', () => {
    expect(btwAnchorKey(null)).toBe('0')
    expect(btwAnchorKey(undefined)).toBe('0')
    expect(btwAnchorKey(Number.NaN)).toBe('0')
  })
})

describe('groupBtwRecords', () => {
  it('groups records by anchor, preserving chronological order within a group', () => {
    const grouped = groupBtwRecords([
      { id: 1, anchorMessageId: 10, question: 'Q1' },
      { id: 2, anchorMessageId: 20, question: 'Q2' },
      { id: 3, anchorMessageId: 10, question: 'Q3' },
    ])
    expect(Object.keys(grouped).sort()).toEqual(['10', '20'])
    expect(grouped['10'].map(r => r.question)).toEqual(['Q1', 'Q3'])
    expect(grouped['20'].map(r => r.question)).toEqual(['Q2'])
  })

  it('groups the pre-message anchor (0) like any other', () => {
    const grouped = groupBtwRecords([
      { id: 1, anchorMessageId: 0, question: 'before anything' },
      { id: 2, anchorMessageId: 0, question: 'also before' },
    ])
    expect(grouped['0']).toHaveLength(2)
  })

  it('returns an empty object for null/undefined input', () => {
    expect(groupBtwRecords(null)).toEqual({})
    expect(groupBtwRecords(undefined)).toEqual({})
    expect(groupBtwRecords([])).toEqual({})
  })

  it('does not merge distinct anchors that stringify differently', () => {
    const grouped = groupBtwRecords([
      { id: 1, anchorMessageId: 1 },
      { id: 2, anchorMessageId: 10 },
      { id: 3, anchorMessageId: 100 },
    ])
    expect(Object.keys(grouped).sort()).toEqual(['1', '10', '100'])
  })
})

describe('messageAnchorKey', () => {
  it('accepts only settled numeric ids', () => {
    expect(messageAnchorKey({ id: 7 })).toBe('7')
  })

  it('rejects optimistic and placeholder ids (not stable enough to key rows by)', () => {
    expect(messageAnchorKey({ id: 'pending-123-abc' })).toBe('')
    expect(messageAnchorKey({ id: 'drain-123' })).toBe('')
    expect(messageAnchorKey({ id: 'local-0' })).toBe('')
    expect(messageAnchorKey({ id: 0 })).toBe('')
    expect(messageAnchorKey({ id: -1 })).toBe('')
    expect(messageAnchorKey({})).toBe('')
    expect(messageAnchorKey(null)).toBe('')
  })
})

describe('anchorCount', () => {
  const anchors = groupBtwRecords([
    { id: 1, anchorMessageId: 10 },
    { id: 2, anchorMessageId: 10 },
    { id: 3, anchorMessageId: 20 },
  ])

  it('counts the questions at a message position', () => {
    expect(anchorCount(anchors, { id: 10 })).toBe(2)
    expect(anchorCount(anchors, { id: 20 })).toBe(1)
  })

  it('returns 0 for a message with no anchor and for unanchorable messages', () => {
    expect(anchorCount(anchors, { id: 30 })).toBe(0)
    expect(anchorCount(anchors, { id: 'pending-1' })).toBe(0)
    expect(anchorCount(anchors, null)).toBe(0)
    expect(anchorCount(null, { id: 10 })).toBe(0)
  })
})

// ── Pending (optimistic) anchors ──
// A question is inserted locally the moment it is asked, so its anchor can show
// an in-progress state without waiting for the server round trip.
describe('anchorKeyPending / anchorPending', () => {
  const settled = { id: 1, anchorMessageId: 10, question: 'q', answer: 'a' }
  const pending = { id: 'btw-pending-1', anchorMessageId: 10, question: 'q', pending: true }

  it('reports pending only while an unanswered entry sits at that anchor', () => {
    expect(anchorKeyPending({ 10: [pending] }, '10')).toBe(true)
    expect(anchorKeyPending({ 10: [settled] }, '10')).toBe(false)
    // A settled entry alongside a pending one still reports pending.
    expect(anchorKeyPending({ 10: [settled, pending] }, '10')).toBe(true)
  })

  it('reports false for an unknown or empty key', () => {
    expect(anchorKeyPending({ 10: [pending] }, '11')).toBe(false)
    expect(anchorKeyPending({}, '10')).toBe(false)
    expect(anchorKeyPending(null, '10')).toBe(false)
    expect(anchorKeyPending({ 10: [pending] }, '')).toBe(false)
    expect(anchorKeyPending({ 10: [pending] }, null)).toBe(false)
  })

  it('resolves the anchor through the same id rule as the count', () => {
    const anchors = { 10: [pending] }
    expect(anchorPending(anchors, { id: 10 })).toBe(true)
    expect(anchorPending(anchors, { id: 11 })).toBe(false)
    // A placeholder id can never match, exactly like anchorCount.
    expect(anchorPending(anchors, { id: 'pending-10' })).toBe(false)
    expect(anchorPending(anchors, null)).toBe(false)
  })
})

describe('currentAnchorKey', () => {
  it('uses the last settled numeric message id', () => {
    expect(currentAnchorKey([{ id: 1 }, { id: 2 }, { id: 3 }])).toBe('3')
  })

  it('skips optimistic messages, which are not in the DB yet', () => {
    // The server anchors to MAX(id) FROM chat_history, so a trailing pending
    // bubble must not become the anchor — it would jump when the answer lands.
    expect(currentAnchorKey([{ id: 7 }, { id: 'db-local-1' }])).toBe('7')
    expect(currentAnchorKey([{ id: 'pending-1' }])).toBe('0')
  })

  it('falls back to the pre-message anchor when nothing is settled', () => {
    expect(currentAnchorKey([])).toBe('0')
    expect(currentAnchorKey(null)).toBe('0')
    expect(currentAnchorKey(undefined)).toBe('0')
  })
})
