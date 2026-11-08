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
        members: [{ id: 'm1', agentId: 'a1', name: 'A', backend: 'claude', disabled: false, isHost: true }],
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
          avatar: '<svg>member</svg>', agentName: 'Claude', disabled: false, isHost: false,
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
          avatar: '', agentName: 'Claude', disabled: false, isHost: false,
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
          avatar: '<svg>member</svg>', agentName: 'Claude', disabled: false, isHost: false,
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
    // Leading @ and specialty in parentheses resolve the same member.
    expect(resolveByName('@Alice')).toEqual({
      name: 'Alice', backend: 'claude', avatar: '<svg>member</svg>', agentName: 'Claude', agentId: 'agent-claude',
    })
    expect(resolveByName('Alice（架构助手）')).toEqual({
      name: 'Alice', backend: 'claude', avatar: '<svg>member</svg>', agentName: 'Claude', agentId: 'agent-claude',
    })
    // Unknown name resolves to null.
    expect(resolveByName('Bob')).toBeNull()
  })

  it('resolveByName prefers an exact name over an earlier bare-prefix member', async () => {
    // Roster order puts the bare `agy` first. A single-pass fuzzy find would
    // strip `(zhu)` from the target and wrongly attribute the chip to `agy`; the
    // exact-first pass must win regardless of order (mirrors lookupMember in
    // internal/service/group_orchestrator.go).
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        members: [
          { id: 'm1', agentId: 'agent-agy', name: 'agy', backend: 'antigravity', disabled: false, isHost: true },
          { id: 'm2', agentId: 'agent-agy', name: 'agy(zhu)', backend: 'antigravity', disabled: false, isHost: false },
        ],
      }),
    })))

    const sid = ref('group-exact-first')
    const { resolveByName } = useGroupMembers(sid)
    await nextTick()
    await new Promise(r => setTimeout(r, 0))

    expect(resolveByName('agy(zhu)')?.name).toBe('agy(zhu)')
    expect(resolveByName('agy(zhu)（架构）')?.name).toBe('agy(zhu)')
    expect(resolveByName('agy')?.name).toBe('agy')
  })

  it('drops a stale response so a slow previous session cannot clobber the current one', async () => {
    // Switch away to a SLOW group, then straight back to the fast one. The slow
    // response lands LAST; without a generation guard it would overwrite the
    // current session's cruise/mode/roster with the previous session's values
    // ("switch away and back and the cruise state is gone").
    const cruiseByGroup: Record<string, boolean> = { 'g-fast': true, 'g-slow': false }
    vi.stubGlobal('fetch', vi.fn(async (url: string) => {
      const gid = String(url).split('groupId=')[1]
      if (gid === 'g-slow') await new Promise(r => setTimeout(r, 50))
      return {
        ok: true,
        json: async () => ({ members: [], cruise: cruiseByGroup[gid] === true }),
      }
    }))

    const sid = ref('g-fast')
    const { cruise } = useGroupMembers(sid)
    await nextTick()
    await new Promise(r => setTimeout(r, 10))
    expect(cruise.value).toBe(true)

    sid.value = 'g-slow' // switch away — slow response now in flight
    await nextTick()
    sid.value = 'g-fast' // switch back — fast response wins the race
    await nextTick()
    await new Promise(r => setTimeout(r, 100)) // let the slow response land last

    expect(cruise.value).toBe(true)
  })

  // A removed member is gone from the roster but its past messages must still
  // show its own name/avatar (the server keeps the row and flags it).
  it('hides removed members from the roster but still resolves their speech', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        members: [
          { id: 'm1', agentId: 'a1', name: 'Host', backend: 'claude', disabled: false, isHost: true },
          { id: 'm2', agentId: 'a2', name: 'Gone', backend: 'claude', disabled: false, isHost: false, removed: true },
        ],
      }),
    })))

    const sid = ref('group-removed')
    const { members, resolveSpeaker, resolveByName } = useGroupMembers(sid)
    await nextTick()
    await new Promise(r => setTimeout(r, 0))

    expect(members.value.map(m => m.id)).toEqual(['m1'])
    expect(resolveSpeaker('m2')?.name).toBe('Gone')
    expect(resolveByName('Gone')?.name).toBe('Gone')
  })
})
