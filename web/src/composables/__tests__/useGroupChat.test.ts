import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { createGroup, listGroupMembers, generateMemberAvatars, addGroupMembers, removeGroupMember, setGroupParallelDefault } from '@/composables/useGroupChat'

vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

// generateMemberAvatars pulls the DiceBear runtime via a dynamic import; stub it
// so the test stays deterministic and never loads a real style chunk.
vi.mock('@/utils/lazyAvatar', () => ({
  generateAvatarSvg: vi.fn(async (seed: string) => `<svg>${seed}</svg>`),
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
    vi.stubGlobal('fetch', mockFetch(200, { ok: true, groupId: 'g1', hostMemberId: 'm1', mode: 'host' }))
    const r = await createGroup('agent-a', ['agent-a', 'agent-b'])
    expect(r).toEqual({ groupId: 'g1', hostMemberId: 'm1', mode: 'host' })
    const [url, opts] = (fetch as any).mock.calls[0]
    expect(url).toBe('/api/group/create')
    expect(JSON.parse(opts.body)).toEqual({ hostAgentId: 'agent-a', memberAgentIds: ['agent-a', 'agent-b'] })
  })

  it('createGroup returns free mode when the backend reports it', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { ok: true, groupId: 'g2', hostMemberId: '', mode: 'free' }))
    const r = await createGroup('', ['agent-a', 'agent-b'])
    expect(r).toEqual({ groupId: 'g2', hostMemberId: '', mode: 'free' })
  })

  it('listGroupMembers returns members, mode and parallelDefault', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { ok: true, members: [{ id: 'm1', isHost: true }], mode: 'free', parallelDefault: true }))
    const res = await listGroupMembers('g1')
    expect(res.members).toEqual([{ id: 'm1', isHost: true }])
    expect(res.mode).toBe('free')
    expect(res.parallelDefault).toBe(true)
  })

  it('listGroupMembers parses parallelDefault, defaulting to false when absent', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { ok: true, members: [], parallelDefault: true }))
    expect((await listGroupMembers('g1')).parallelDefault).toBe(true)

    vi.stubGlobal('fetch', mockFetch(200, { ok: true, members: [] }))
    expect((await listGroupMembers('g1')).parallelDefault).toBe(false)
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

  it('setGroupParallelDefault PATCHes parallelDefault', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { ok: true }))
    await setGroupParallelDefault('g1', true)
    const [url, opts] = (fetch as any).mock.calls[0]
    expect(url).toBe('/api/group/settings')
    expect(opts.method).toBe('PATCH')
    expect(JSON.parse(opts.body)).toEqual({ groupId: 'g1', parallelDefault: true })
  })

  it('throws on a non-ok response', async () => {
    vi.stubGlobal('fetch', mockFetch(500, {}))
    await expect(createGroup('agent-a', ['agent-a'])).rejects.toThrow()
  })
})

describe('generateMemberAvatars', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  // Route GET /api/group/members to `members` and capture the POST body sent to
  // /api/group/member-avatars. Throws on any other URL so an unexpected call
  // (e.g. a stray POST when there is nothing to backfill) fails the test.
  function stubRoutes(members: unknown[], onPost?: (body: any) => void) {
    vi.stubGlobal('fetch', vi.fn(async (url: string, opts?: any) => {
      if (url.startsWith('/api/group/members')) {
        return { ok: true, status: 200, json: async () => ({ members }) }
      }
      if (url === '/api/group/member-avatars') {
        onPost?.(JSON.parse(opts.body))
        return { ok: true, status: 200, json: async () => ({}) }
      }
      throw new Error(`unexpected fetch: ${url}`)
    }))
  }

  it('posts a generated avatar for every member that lacks one', async () => {
    let posted: any
    stubRoutes(
      [{ id: 'm1', name: 'Alice', avatar: '' }, { id: 'm2', name: 'Bob', avatar: '' }],
      body => { posted = body },
    )
    await generateMemberAvatars('g1')
    expect(posted).toEqual({
      groupId: 'g1',
      avatars: [
        { memberId: 'm1', avatar: '<svg>Alice</svg>' },
        { memberId: 'm2', avatar: '<svg>Bob</svg>' },
      ],
    })
  })

  it('skips members that already have an avatar', async () => {
    let posted: any
    stubRoutes(
      [{ id: 'm1', name: 'Alice', avatar: '<svg>existing</svg>' }, { id: 'm2', name: 'Bob', avatar: '' }],
      body => { posted = body },
    )
    await generateMemberAvatars('g1')
    expect(posted.avatars).toEqual([{ memberId: 'm2', avatar: '<svg>Bob</svg>' }])
  })

  it('scopes to onlyMemberIds when provided', async () => {
    let posted: any
    stubRoutes(
      [{ id: 'm1', name: 'Alice', avatar: '' }, { id: 'm2', name: 'Bob', avatar: '' }],
      body => { posted = body },
    )
    await generateMemberAvatars('g1', ['m2'])
    expect(posted.avatars).toEqual([{ memberId: 'm2', avatar: '<svg>Bob</svg>' }])
  })

  it('does not POST when there is nothing to backfill (idempotent)', async () => {
    stubRoutes([{ id: 'm1', name: 'Alice', avatar: '<svg>existing</svg>' }])
    await generateMemberAvatars('g1')
    // Only the roster GET happened.
    expect((fetch as any).mock.calls).toHaveLength(1)
    expect((fetch as any).mock.calls[0][0]).toContain('/api/group/members')
  })
})
