import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { createGroup, listGroupMembers, generateMemberAvatars, addGroupMembers, removeGroupMember, setGroupParallelDefault, setGroupCruise } from '@/composables/useGroupChat'

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

  it('listGroupMembers parses cruise, defaulting to false when absent', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { ok: true, members: [], cruise: true }))
    expect((await listGroupMembers('g1')).cruise).toBe(true)

    vi.stubGlobal('fetch', mockFetch(200, { ok: true, members: [] }))
    expect((await listGroupMembers('g1')).cruise).toBe(false)
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

  it('setGroupCruise PATCHes cruise', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { ok: true }))
    await setGroupCruise('g1', true)
    const [url, opts] = (fetch as any).mock.calls[0]
    expect(url).toBe('/api/group/settings')
    expect(opts.method).toBe('PATCH')
    expect(JSON.parse(opts.body)).toEqual({ groupId: 'g1', cruise: true })
  })

  it('setGroupCruise throws on a non-ok response', async () => {
    vi.stubGlobal('fetch', mockFetch(500, {}))
    await expect(setGroupCruise('g1', false)).rejects.toThrow()
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

  /** rosterCalls returns the URLs of the requests made (the GET plus any POST). */
  function fetchUrls(): string[] {
    return (fetch as any).mock.calls.map((c: any[]) => c[0])
  }

  // A lone agent keeps the agent's own configured avatar: there is only one
  // copy, so there is nothing to tell apart and no generated avatar is written.
  it('does not generate an avatar for a lone agent', async () => {
    stubRoutes([{ id: 'm1', agentId: 'a1', name: 'Claude', avatar: '' }])
    await generateMemberAvatars('g1')
    // Only the roster GET happened — no POST.
    expect(fetchUrls()).toHaveLength(1)
    expect(fetchUrls()[0]).toContain('/api/group/members')
  })

  // Distinct agents, each added once: no generated avatars anywhere.
  it('does not generate avatars when every agent is added once', async () => {
    stubRoutes([
      { id: 'm1', agentId: 'a1', name: 'Claude', avatar: '' },
      { id: 'm2', agentId: 'a2', name: 'Codex', avatar: '' },
    ])
    await generateMemberAvatars('g1')
    expect(fetchUrls()).toHaveLength(1)
  })

  // The same agent added twice: only the SECOND instance gets a generated
  // avatar; the first keeps the agent avatar.
  it('generates an avatar only for the second instance of a repeated agent', async () => {
    let posted: any
    stubRoutes(
      [
        { id: 'm1', agentId: 'a1', name: 'Claude', avatar: '' },
        { id: 'm2', agentId: 'a1', name: 'Alice', avatar: '' },
      ],
      body => { posted = body },
    )
    await generateMemberAvatars('g1')
    expect(posted).toEqual({
      groupId: 'g1',
      avatars: [{ memberId: 'm2', avatar: '<svg>Alice</svg>' }],
    })
  })

  // A third instance also gets one; the first still does not.
  it('generates for every instance after the first', async () => {
    let posted: any
    stubRoutes(
      [
        { id: 'm1', agentId: 'a1', name: 'Claude', avatar: '' },
        { id: 'm2', agentId: 'a1', name: 'Alice', avatar: '' },
        { id: 'm3', agentId: 'a1', name: 'Bob', avatar: '' },
      ],
      body => { posted = body },
    )
    await generateMemberAvatars('g1')
    expect(posted.avatars).toEqual([
      { memberId: 'm2', avatar: '<svg>Alice</svg>' },
      { memberId: 'm3', avatar: '<svg>Bob</svg>' },
    ])
  })

  // The first instance already has an avatar (from a previous run); the second
  // still needs one, and the first is never re-generated.
  it('skips instances that already have an avatar', async () => {
    let posted: any
    stubRoutes(
      [
        { id: 'm1', agentId: 'a1', name: 'Claude', avatar: '<svg>existing</svg>' },
        { id: 'm2', agentId: 'a1', name: 'Alice', avatar: '' },
      ],
      body => { posted = body },
    )
    await generateMemberAvatars('g1')
    expect(posted.avatars).toEqual([{ memberId: 'm2', avatar: '<svg>Alice</svg>' }])
  })

  // onlyMemberIds scopes the backfill to freshly added members: a new SECOND
  // instance is generated, a pre-existing first is untouched.
  it('scopes to onlyMemberIds when provided', async () => {
    let posted: any
    stubRoutes(
      [
        { id: 'm1', agentId: 'a1', name: 'Claude', avatar: '' },
        { id: 'm2', agentId: 'a1', name: 'Alice', avatar: '' },
      ],
      body => { posted = body },
    )
    await generateMemberAvatars('g1', ['m2'])
    expect(posted.avatars).toEqual([{ memberId: 'm2', avatar: '<svg>Alice</svg>' }])
  })

  // A newly added FIRST instance (the agent had no active copy) keeps the agent
  // avatar even when it is the only id in scope.
  it('does not generate for a newly added first instance', async () => {
    stubRoutes([{ id: 'm1', agentId: 'a1', name: 'Claude', avatar: '' }])
    await generateMemberAvatars('g1', ['m1'])
    expect(fetchUrls()).toHaveLength(1)
  })

  // A left (archived) copy does not count: the remaining active instance is the
  // first and keeps the agent avatar.
  it('ignores left members when deciding the first instance', async () => {
    stubRoutes([
      { id: 'm0', agentId: 'a1', name: 'Old', avatar: '', left: true },
      { id: 'm1', agentId: 'a1', name: 'Claude', avatar: '' },
    ])
    await generateMemberAvatars('g1')
    expect(fetchUrls()).toHaveLength(1)
  })

  it('does not POST when there is nothing to backfill (idempotent)', async () => {
    stubRoutes([{ id: 'm1', agentId: 'a1', name: 'Alice', avatar: '<svg>existing</svg>' }])
    await generateMemberAvatars('g1')
    // Only the roster GET happened.
    expect(fetchUrls()).toHaveLength(1)
    expect(fetchUrls()[0]).toContain('/api/group/members')
  })
})
