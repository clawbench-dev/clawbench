import { describe, expect, it } from 'vitest'
import {
  buildSessionForkTree,
  resolveForkAnchor,
  visibleForkRows,
  type ForkTreeSession,
} from '@/utils/sessionForkTree.ts'

/**
 * The grouping rules are pinned one test each, because every one of them was a
 * deliberate call with a counter-argument (see the module header): grouping
 * across the pinned boundary, refusing to group an orphaned chain, mistaking
 * the `acp:` marker for a session id, and looping on a cycle. A test that only
 * covered the happy chain would let any of them regress silently.
 */

/** Build a session. `src` is the source_session_id. */
function s(id: string, src?: string, pinned = false): ForkTreeSession {
  return src === undefined ? { id, pinned } : { id, sourceSessionId: src, pinned }
}

/** ids of a group's members, in rendered order. */
function memberIds(grouping: ReturnType<typeof buildSessionForkTree>, anchorId: string) {
  return (grouping.membersByAnchor.get(anchorId) ?? []).map(r => r.session.id)
}

/** ids of the top-level rows, in rendered order. */
function topIds(grouping: ReturnType<typeof buildSessionForkTree>) {
  return grouping.topSessions.map(s => s.id)
}

describe('resolveForkAnchor', () => {
  it('returns the session itself when it has no source', () => {
    const byId = new Map([['a', s('a')]])
    expect(resolveForkAnchor(byId.get('a')!, byId)).toBe('a')
  })

  it('walks up a chain to the root', () => {
    const sessions = [s('a'), s('a1', 'a'), s('a2', 'a1'), s('a3', 'a2')]
    const byId = new Map(sessions.map(x => [x.id, x]))
    expect(resolveForkAnchor(byId.get('a3')!, byId)).toBe('a')
  })

  it('stops at the highest LOADED ancestor when the root is gone', () => {
    // The issue's own case: the root was hard-deleted, so a1's parent is
    // missing. a1 becomes the anchor rather than the whole chain being dropped.
    const sessions = [s('a1', 'gone'), s('a2', 'a1'), s('a3', 'a2')]
    const byId = new Map(sessions.map(x => [x.id, x]))
    expect(resolveForkAnchor(byId.get('a3')!, byId)).toBe('a1')
  })

  it('never climbs across the pinned boundary', () => {
    // A pinned child of an unpinned parent anchors itself: `pinned DESC` would
    // lift it out of the parent's group, so a group spanning the boundary could
    // not stay contiguous and its count would lie.
    const sessions = [s('a'), s('a1', 'a', true)]
    const byId = new Map(sessions.map(x => [x.id, x]))
    expect(resolveForkAnchor(byId.get('a1')!, byId)).toBe('a1')
  })

  it('still climbs within a pinned chain', () => {
    const sessions = [s('a', undefined, true), s('a1', 'a', true), s('a2', 'a1', true)]
    const byId = new Map(sessions.map(x => [x.id, x]))
    expect(resolveForkAnchor(byId.get('a2')!, byId)).toBe('a')
  })

  it('treats an "acp:" source as no source at all', () => {
    // ServeACPLoadSession overloads the column with the marker string, not an
    // id — looking it up must not accidentally match a session named that way.
    const sessions = [s('a'), s('loaded', 'acp:a')]
    const byId = new Map(sessions.map(x => [x.id, x]))
    expect(resolveForkAnchor(byId.get('loaded')!, byId)).toBe('loaded')
  })

  it('survives a cycle without hanging', () => {
    const sessions = [s('a', 'b'), s('b', 'a')]
    const byId = new Map(sessions.map(x => [x.id, x]))
    // Each walks into the other; the visited set stops the walk and each ends
    // as its own anchor, so both stay visible.
    expect(resolveForkAnchor(byId.get('a')!, byId)).toBe('a')
    expect(resolveForkAnchor(byId.get('b')!, byId)).toBe('b')
  })
})

describe('buildSessionForkTree', () => {
  it('keeps a plain list untouched', () => {
    const grouping = buildSessionForkTree([s('a'), s('b'), s('c')])
    expect(topIds(grouping)).toEqual(['a', 'b', 'c'])
    expect(grouping.membersByAnchor.size).toBe(0)
  })

  it('folds a chain under its root and numbers the generations', () => {
    const grouping = buildSessionForkTree([s('a'), s('a1', 'a'), s('a2', 'a1'), s('a3', 'a2')])
    expect(topIds(grouping)).toEqual(['a'])
    expect(memberIds(grouping, 'a')).toEqual(['a1', 'a2', 'a3'])
    expect(grouping.membersByAnchor.get('a')!.map(r => r.depth)).toEqual([1, 2, 3])
  })

  it('numbers generations by lineage, not by array position', () => {
    // A fresh fork gets sort_order 0, so the server hands it to us ABOVE its
    // parent. A single forward pass would read the parent's generation before
    // computing it; the walk-up must get this right.
    const grouping = buildSessionForkTree([s('a2', 'a1'), s('a'), s('a1', 'a')])
    expect(topIds(grouping)).toEqual(['a'])
    const members = grouping.membersByAnchor.get('a')!
    // Sorted shallowest-first for the indent, with each depth still correct.
    expect(members.map(r => [r.session.id, r.depth])).toEqual([['a1', 1], ['a2', 2]])
  })

  it('groups every branch under one root', () => {
    // Forking the same session twice produces siblings, not a chain.
    const grouping = buildSessionForkTree([s('a'), s('a1', 'a'), s('a2', 'a')])
    expect(topIds(grouping)).toEqual(['a'])
    expect(memberIds(grouping, 'a')).toEqual(['a1', 'a2'])
    expect(grouping.membersByAnchor.get('a')!.map(r => r.depth)).toEqual([1, 1])
  })

  it('promotes an orphaned chain to its own group', () => {
    const grouping = buildSessionForkTree([s('x'), s('a1', 'gone'), s('a2', 'a1')])
    expect(topIds(grouping)).toEqual(['x', 'a1'])
    expect(memberIds(grouping, 'a1')).toEqual(['a2'])
  })

  it('reports a group size that matches what it renders', () => {
    const grouping = buildSessionForkTree([s('a'), s('a1', 'a'), s('a2', 'a1')])
    expect(grouping.membersByAnchor.get('a')!.length).toBe(2)
  })

  it('exposes group members through byId so the row menu can resolve them', () => {
    const grouping = buildSessionForkTree([s('a'), s('a1', 'a')])
    expect(grouping.byId.get('a1')?.id).toBe('a1')
  })

  it('does not mutate the input array', () => {
    const input = [s('a'), s('a1', 'a')]
    buildSessionForkTree(input)
    expect(input.map(x => x.id)).toEqual(['a', 'a1'])
  })
})

describe('visibleForkRows', () => {
  const grouping = buildSessionForkTree([s('a'), s('a1', 'a'), s('a2', 'a1'), s('b')])

  it('renders an expanded group inline after its anchor', () => {
    const rows = visibleForkRows(grouping.topSessions, grouping.membersByAnchor, () => false)
    expect(rows.map(r => [r.session.id, r.depth])).toEqual([
      ['a', 0], ['a1', 1], ['a2', 2], ['b', 0],
    ])
  })

  it('omits a collapsed group entirely, keeping its count on the anchor', () => {
    const rows = visibleForkRows(grouping.topSessions, grouping.membersByAnchor, id => id === 'a')
    expect(rows.map(r => r.session.id)).toEqual(['a', 'b'])
    expect(rows[0].isAnchor).toBe(true)
    expect(rows[0].childCount).toBe(2)
  })

  it('marks a leaf as not-anchor with no count', () => {
    const rows = visibleForkRows(grouping.topSessions, grouping.membersByAnchor, () => false)
    const leaf = rows.find(r => r.session.id === 'b')!
    expect(leaf.isAnchor).toBe(false)
    expect(leaf.childCount).toBe(0)
  })
})
