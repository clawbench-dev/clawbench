import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { createGroup, listGroupMembers, addGroupMembers, removeGroupMember, updateGroupSettings } from '@/composables/useGroupChat'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

function mockFetch(status: number, body: unknown) {
  return vi.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  })
}

describe('useGroupChat API', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', mockFetch(200, {}))
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('createGroup posts hostAgentId + memberAgentIds and returns ids', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { ok: true, groupId: 'g1', hostMemberId: 'm1' }))
    const r = await createGroup('agent-a', ['agent-a', 'agent-b'])
    expect(r).toEqual({ groupId: 'g1', hostMemberId: 'm1' })
    const [url, opts] = (fetch as any).mock.calls[0]
    expect(url).toBe('/api/group/create')
    expect(JSON.parse(opts.body)).toEqual({ hostAgentId: 'agent-a', memberAgentIds: ['agent-a', 'agent-b'] })
  })

  it('listGroupMembers returns members and maxRounds', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { ok: true, members: [{ id: 'm1', isHost: true }], maxRounds: 4 }))
    const res = await listGroupMembers('g1')
    expect(res.members).toEqual([{ id: 'm1', isHost: true }])
    expect(res.maxRounds).toBe(4)
  })

  it('listGroupMembers defaults maxRounds to 10 when absent', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { ok: true, members: [] }))
    const res = await listGroupMembers('g1')
    expect(res.maxRounds).toBe(10)
  })

  it('addGroupMembers posts an array and returns memberIds', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { ok: true, memberIds: ['m2'] }))
    const ids = await addGroupMembers('g1', ['agent-b'])
    expect(ids).toEqual(['m2'])
    const [url, opts] = (fetch as any).mock.calls[0]
    expect(url).toBe('/api/group/members')
    expect(JSON.parse(opts.body)).toEqual({ groupId: 'g1', agentIds: ['agent-b'] })
  })

  it('removeGroupMember uses DELETE', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { ok: true }))
    await removeGroupMember('g1', 'm2')
    const [, opts] = (fetch as any).mock.calls[0]
    expect(opts.method).toBe('DELETE')
  })

  it('updateGroupSettings uses PATCH', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { ok: true }))
    await updateGroupSettings('g1', 8)
    const [url, opts] = (fetch as any).mock.calls[0]
    expect(url).toBe('/api/group/settings')
    expect(opts.method).toBe('PATCH')
    expect(JSON.parse(opts.body)).toEqual({ groupId: 'g1', maxRounds: 8 })
  })

  it('throws on a non-ok response', async () => {
    vi.stubGlobal('fetch', mockFetch(500, {}))
    await expect(createGroup('agent-a', ['agent-a'])).rejects.toThrow()
  })
})
