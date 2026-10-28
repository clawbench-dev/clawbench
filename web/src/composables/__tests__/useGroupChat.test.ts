import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { createGroup, listGroupMembers, generateMemberAvatars, addGroupMembers, setGroupMemberDisabled, setGroupParallelDefault, setGroupCruise, compactGroupMember } from '@/composables/useGroupChat'

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

  it('setGroupMemberDisabled PATCHes the disabled flag', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { ok: true }))
    await setGroupMemberDisabled('g1', 'm2', true)
    const [url, opts] = (fetch as any).mock.calls[0]
    expect(url).toBe('/api/group/members')
    expect(opts.method).toBe('PATCH')
    expect(JSON.parse(opts.body)).toEqual({ groupId: 'g1', memberId: 'm2', disabled: true })
  })

  it('setGroupMemberDisabled can enable (disabled:false)', async () => {
    vi.stubGlobal('fetch', mockFetch(200, { ok: true }))
    await setGroupMemberDisabled('g1', 'm2', false)
    const [, opts] = (fetch as any).mock.calls[0]
    expect(JSON.parse(opts.body)).toEqual({ groupId: 'g1', memberId: 'm2', disabled: false })
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

  it('listGroupMembers passes member usage through verbatim', async () => {
    // The roster must distinguish "never reported" (null) from "0% used", so a
    // `?? {}` fallback here would silently turn one into the other.
    const usage = { inputTokens: 1200, totalTokens: 1500, contextWindow: 200000 }
    vi.stubGlobal('fetch', mockFetch(200, {
      ok: true,
      members: [
        { id: 'm1', usage },
        { id: 'm2', usage: null },
        { id: 'm3' },
      ],
    }))
    const { members } = await listGroupMembers('g1')
    expect(members[0].usage).toEqual(usage)
    expect(members[1].usage).toBeNull()
    expect(members[2].usage).toBeUndefined()
  })

  it('compactGroupMember POSTs groupId + memberId and returns the usage', async () => {
    const usage = { inputTokens: 900, totalTokens: 1000, contextWindow: 200000 }
    vi.stubGlobal('fetch', mockFetch(200, { ok: true, usage }))
    const got = await compactGroupMember('g1', 'm2')
    expect(got).toEqual(usage)
    const [url, opts] = (fetch as any).mock.calls[0]
    expect(url).toBe('/api/group/member-compact')
    expect(opts.method).toBe('POST')
    expect(JSON.parse(opts.body)).toEqual({ groupId: 'g1', memberId: 'm2' })
  })

  it('compactGroupMember returns null when the server reports no usage', async () => {
    // A CLI-transport member never reports usage; the caller shows "unknown"
    // rather than a 0% bar, so the null must survive the round trip.
    vi.stubGlobal('fetch', mockFetch(200, { ok: true, usage: null }))
    expect(await compactGroupMember('g1', 'm2')).toBeNull()
  })

  it('compactGroupMember surfaces the server error message', async () => {
    vi.stubGlobal('fetch', mockFetch(409, { error: 'group is running' }))
    await expect(compactGroupMember('g1', 'm2')).rejects.toThrow('group is running')
  })

  it('compactGroupMember falls back to the status when the body is not JSON', async () => {
    // A proxy/500 HTML page must not turn into "undefined" in the toast.
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 502,
      json: async () => { throw new Error('not json') },
    }))
    await expect(compactGroupMember('g1', 'm2')).rejects.toThrow('request failed: 502')
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

  // A disabled copy does not count: the remaining speaking instance is the first
  // and keeps the agent avatar.
  it('ignores disabled members when deciding the first instance', async () => {
    stubRoutes([
      { id: 'm0', agentId: 'a1', name: 'Old', avatar: '', disabled: true },
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
