import { describe, it, expect } from 'vitest'
import { collectGroupMemberStatus, type GroupMemberStatusMessage } from '../groupMemberStatus'

const members = [
  { id: 'h' },
  { id: 'b' },
  { id: 'c' },
]

/** Assistant reply from member `id`. */
let seq = 0
function reply(id: string, extra: Partial<GroupMemberStatusMessage> = {}): GroupMemberStatusMessage {
  seq += 1
  return { role: 'assistant', agentId: id, id: `m-${id}-${seq}`, ...extra }
}

const userMsg: GroupMemberStatusMessage = { role: 'user', id: 'u1' }

function statusOf(entries: ReturnType<typeof collectGroupMemberStatus>, id: string) {
  return entries.find(e => e.id === id)?.status
}

describe('collectGroupMemberStatus', () => {
  it('marks a streaming reply as speaking', () => {
    const entries = collectGroupMemberStatus(members, [userMsg, reply('b', { streaming: true })])
    expect(statusOf(entries, 'b')).toBe('speaking')
    expect(statusOf(entries, 'c')).toBe('waiting')
  })

  it('supports several members speaking at the same time (free mode)', () => {
    const entries = collectGroupMemberStatus(members, [
      userMsg,
      reply('b', { streaming: true }),
      reply('c', { streaming: true }),
    ])
    expect(statusOf(entries, 'b')).toBe('speaking')
    expect(statusOf(entries, 'c')).toBe('speaking')
  })

  it('marks a finished reply in this round as done', () => {
    const entries = collectGroupMemberStatus(members, [userMsg, reply('b')])
    expect(statusOf(entries, 'b')).toBe('done')
  })

  it('does not count a reply from the PREVIOUS round as done', () => {
    const entries = collectGroupMemberStatus(members, [userMsg, reply('b'), userMsg])
    expect(statusOf(entries, 'b')).toBe('waiting')
  })

  it('uses the last user message as the round boundary (not the first)', () => {
    const entries = collectGroupMemberStatus(members, [
      userMsg,
      reply('b'),
      userMsg,
      reply('c'),
    ])
    expect(statusOf(entries, 'b')).toBe('waiting')
    expect(statusOf(entries, 'c')).toBe('done')
  })

  it('treats the whole list as the round when there is no user message', () => {
    // A background / cruise-only run has no user message anchoring it.
    const entries = collectGroupMemberStatus(members, [reply('b')])
    expect(statusOf(entries, 'b')).toBe('done')
  })

  it('marks a disabled member as disabled even when it replied', () => {
    const entries = collectGroupMemberStatus(
      [{ id: 'b', disabled: true }],
      [userMsg, reply('b')],
    )
    expect(statusOf(entries, 'b')).toBe('disabled')
  })

  it('prefers stopped over speaking (the optimistic badge is written while the bubble still streams)', () => {
    const entries = collectGroupMemberStatus(members, [
      userMsg,
      reply('b', { streaming: true, stopped: true }),
    ])
    expect(statusOf(entries, 'b')).toBe('stopped')
  })

  it('treats a whole-turn cancel as stopped, not done', () => {
    const entries = collectGroupMemberStatus(members, [
      userMsg,
      reply('b', { cancelled: true }),
    ])
    expect(statusOf(entries, 'b')).toBe('stopped')
  })

  it('follows the LATEST reply when a member spoke twice in one round', () => {
    const entries = collectGroupMemberStatus(members, [
      userMsg,
      reply('b', { stopped: true }),
      reply('b', { streaming: true }),
    ])
    expect(statusOf(entries, 'b')).toBe('speaking')
  })

  it('ignores messages without an agent id (system rows)', () => {
    const entries = collectGroupMemberStatus(members, [
      userMsg,
      { role: 'system', id: 's1', content: 'member joined' },
    ])
    expect(entries.map(e => e.status)).toEqual(['waiting', 'waiting', 'waiting'])
  })

  it('reports the latest reply in the WHOLE session as the jump target', () => {
    const entries = collectGroupMemberStatus(members, [
      userMsg,
      reply('b', { id: 11 }),
      userMsg,
      reply('b', { id: 22 }),
    ])
    expect(entries.find(e => e.id === 'b')?.lastMessageId).toBe(22)
    expect(entries.find(e => e.id === 'b')?.lastMessageAt).toBe('')
  })

  it('keeps a jump target for a member who only spoke in an earlier round', () => {
    const entries = collectGroupMemberStatus(members, [
      userMsg,
      reply('b', { id: 7, createdAt: '2026-11-12T10:00:00Z' }),
      userMsg,
    ])
    const b = entries.find(e => e.id === 'b')
    expect(b?.status).toBe('waiting')
    expect(b?.lastMessageId).toBe(7)
    expect(b?.lastMessageAt).toBe('2026-11-12T10:00:00Z')
  })

  it('has no jump target for a member who never spoke', () => {
    const entries = collectGroupMemberStatus(members, [userMsg])
    expect(entries.find(e => e.id === 'c')?.lastMessageId).toBeNull()
  })

  it('keeps the roster order', () => {
    const entries = collectGroupMemberStatus(
      [{ id: 'z' }, { id: 'a' }],
      [],
    )
    expect(entries.map(e => e.id)).toEqual(['z', 'a'])
  })

  it('tolerates empty input', () => {
    expect(collectGroupMemberStatus(null, null)).toEqual([])
    expect(collectGroupMemberStatus([], [])).toEqual([])
  })
})
