import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { useGroupMembers } from '@/composables/useGroupMembers'

// useGroupMembers loads the roster + mode + parallelDefault for the current
// group session. The member-speech cap is a global setting, not returned here.
describe('useGroupMembers', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
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
})
