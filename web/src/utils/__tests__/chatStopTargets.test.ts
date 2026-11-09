import { describe, expect, it } from 'vitest'
import { collectStopTargets } from '@/utils/chatStopTargets'

function base(overrides: Record<string, unknown> = {}) {
  return {
    isGroupSession: true,
    hostMemberId: 'host-1',
    roster: [
      { id: 'm-1', name: '小北', backend: 'claude', avatar: '<svg/>' },
      { id: 'm-2', name: '阿岚', backend: 'claude' },
    ],
    resolveSpeaker: null,
    fallbackName: '成员',
    ...overrides,
  }
}

describe('collectStopTargets', () => {
  it('lists only assistant messages that are streaming', () => {
    const targets = collectStopTargets([
      { role: 'assistant', streaming: true, agentId: 'm-1' },
      { role: 'assistant', agentId: 'm-2' }, // finished
      { role: 'user', streaming: true, agentId: 'm-3' }, // never a speaker
      { role: 'assistant', streaming: true }, // no speaker id
    ], base())
    expect(targets.map((t) => t.id)).toEqual(['m-1'])
  })

  it('is empty outside a group chat', () => {
    const targets = collectStopTargets(
      [{ role: 'assistant', streaming: true, agentId: 'm-1' }],
      base({ isGroupSession: false }),
    )
    expect(targets).toEqual([])
  })

  // The host routes the round, so stopping it ends the turn instead of silencing
  // one voice. It must never be offered as a "stop this member" row.
  it('excludes the host', () => {
    const targets = collectStopTargets([
      { role: 'assistant', streaming: true, agentId: 'host-1' },
      { role: 'assistant', streaming: true, agentId: 'm-1' },
    ], base())
    expect(targets.map((t) => t.id)).toEqual(['m-1'])
  })

  it('does not exclude anyone when the group has no host (free mode)', () => {
    const targets = collectStopTargets(
      [{ role: 'assistant', streaming: true, agentId: 'm-1' }],
      base({ hostMemberId: '' }),
    )
    expect(targets.map((t) => t.id)).toEqual(['m-1'])
  })

  it('prefers the roster name, then the resolver, then the fallback', () => {
    const targets = collectStopTargets([
      { role: 'assistant', streaming: true, agentId: 'm-1' }, // in roster
      { role: 'assistant', streaming: true, agentId: 'm-9' }, // resolver only
      { role: 'assistant', streaming: true, agentId: 'm-x' }, // unknown
    ], base({
      resolveSpeaker: (id: string) => (id === 'm-9' ? { name: '解析名', backend: 'codex' } : null),
    }))
    expect(targets[0].name).toBe('小北')
    expect(targets[1].name).toBe('解析名')
    expect(targets[1].backend).toBe('codex')
    expect(targets[2].name).toBe('成员')
  })

  it('carries the avatar from the roster when the resolver has none', () => {
    const targets = collectStopTargets(
      [{ role: 'assistant', streaming: true, agentId: 'm-1' }],
      base(),
    )
    expect(targets[0].avatar).toBe('<svg/>')
  })

  it('parses the speech start time and tolerates a missing one', () => {
    const targets = collectStopTargets([
      { role: 'assistant', streaming: true, agentId: 'm-1', createdAt: '2026-11-09T10:00:00Z' },
      { role: 'assistant', streaming: true, agentId: 'm-2' },
    ], base())
    expect(targets[0].startedAt).toBe(Date.parse('2026-11-09T10:00:00Z'))
    expect(targets[1].startedAt).toBe(0)
  })

  it('falls back to the roster when the resolver is not a function', () => {
    const targets = collectStopTargets(
      [{ role: 'assistant', streaming: true, agentId: 'm-1' }],
      base({ resolveSpeaker: { not: 'a function' } }),
    )
    expect(targets[0].name).toBe('小北')
  })
})
