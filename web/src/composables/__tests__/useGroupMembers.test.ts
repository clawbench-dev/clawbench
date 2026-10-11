import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { useGroupMembers } from '@/composables/useGroupMembers'

// getAgentAvatar is the member's fallback avatar source when the member row
// carries no snapshot of its own. Keep it controllable per test.
const { agentAvatars } = vi.hoisted(() => ({ agentAvatars: {} as Record<string, string> }))
vi.mock('@/composables/useAgents', () => ({
  getAgentAvatar: (id: string) => agentAvatars[id] || '',
  useAgents: () => ({ loadAgents: vi.fn(async () => {}) }),
}))

// useGroupMembers loads the roster + mode + parallelDefault for the current
// group session. The member-speech cap is a global setting, not returned here.
describe('useGroupMembers', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
    for (const k of Object.keys(agentAvatars)) delete agentAvatars[k]
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('exposes the roster', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        members: [{ id: 'm1', agentId: 'a1', name: 'A', backend: 'claude', left: false, isHost: true }],
      }),
    })))

    const sid = ref('group-1')
    const { members } = useGroupMembers(sid)
    await nextTick()
    await new Promise(r => setTimeout(r, 0))

    expect(members.value).toHaveLength(1)
  })

  it('does not expose a per-group maxRounds (the cap is global)', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ members: [], maxRounds: 4 }),
    })))

    const sid = ref('group-2')
    const result = useGroupMembers(sid) as Record<string, unknown>
    await nextTick()
    await new Promise(r => setTimeout(r, 0))

    expect(result.maxRounds).toBeUndefined()
  })

  it('exposes parallelDefault from the server and defaults it to false', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ members: [], parallelDefault: true }),
    })))

    const sid = ref('group-3')
    const { parallelDefault } = useGroupMembers(sid)
    await nextTick()
    await new Promise(r => setTimeout(r, 0))
    expect(parallelDefault.value).toBe(true)

    // A response without the field (older backend) reads as sequential.
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ members: [] }),
    })))
    const sid2 = ref('group-4')
    const { parallelDefault: pd2 } = useGroupMembers(sid2)
    await nextTick()
    await new Promise(r => setTimeout(r, 0))
    expect(pd2.value).toBe(false)
  })

  it('resolveSpeaker prefers the member avatar snapshot and exposes agentName', async () => {
    agentAvatars['agent-claude'] = '<svg>agent</svg>'
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        members: [{
          id: 'm1', agentId: 'agent-claude', name: 'Alice', backend: 'claude',
          avatar: '<svg>member</svg>', agentName: 'Claude', left: false, isHost: false,
        }],
      }),
    })))

    const sid = ref('group-speaker')
    const { resolveSpeaker } = useGroupMembers(sid)
    await nextTick()
    await new Promise(r => setTimeout(r, 0))

    expect(resolveSpeaker('m1')).toEqual({
      name: 'Alice', backend: 'claude', avatar: '<svg>member</svg>', agentName: 'Claude', agentId: 'agent-claude',
    })
  })

  it('resolveSpeaker falls back to the agent avatar when the member has none', async () => {
    agentAvatars['agent-claude'] = '<svg>agent</svg>'
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        members: [{
          id: 'm1', agentId: 'agent-claude', name: 'Alice', backend: 'claude',
          avatar: '', agentName: 'Claude', left: false, isHost: false,
        }],
      }),
    })))

    const sid = ref('group-speaker-fallback')
    const { resolveSpeaker } = useGroupMembers(sid)
    await nextTick()
    await new Promise(r => setTimeout(r, 0))

    expect(resolveSpeaker('m1')).toEqual({
      name: 'Alice', backend: 'claude', avatar: '<svg>agent</svg>', agentName: 'Claude', agentId: 'agent-claude',
    })
  })

  it('resolveByName prefers the member avatar snapshot and exposes agentName', async () => {
    agentAvatars['agent-claude'] = '<svg>agent</svg>'
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        members: [{
          id: 'm1', agentId: 'agent-claude', name: 'Alice', backend: 'claude',
          avatar: '<svg>member</svg>', agentName: 'Claude', left: false, isHost: false,
        }],
      }),
    })))

    const sid = ref('group-by-name')
    const { resolveByName } = useGroupMembers(sid)
    await nextTick()
    await new Promise(r => setTimeout(r, 0))

    expect(resolveByName('Alice')).toEqual({
      name: 'Alice', backend: 'claude', avatar: '<svg>member</svg>', agentName: 'Claude', agentId: 'agent-claude',
    })
    // Trimmed input resolves the same member.
    expect(resolveByName('  Alice  ')).toEqual({
      name: 'Alice', backend: 'claude', avatar: '<svg>member</svg>', agentName: 'Claude', agentId: 'agent-claude',
    })
    // Unknown name resolves to null.
    expect(resolveByName('Bob')).toBeNull()
  })
})
