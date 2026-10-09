import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { useGroupMembers } from '@/composables/useGroupMembers'

// useGroupMembers loads the roster + maxRounds for the current group session.
// The maxRounds read-back exists so the member sheet shows the SERVER value
// instead of a hardcoded default (the PATCH endpoint has no other read path).
describe('useGroupMembers', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('exposes the roster and the server maxRounds', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({
        members: [{ id: 'm1', agentId: 'a1', name: 'A', backend: 'claude', left: false, isHost: true }],
        maxRounds: 4,
      }),
    })))

    const sid = ref('group-1')
    const { members, maxRounds } = useGroupMembers(sid)
    await nextTick()
    await new Promise(r => setTimeout(r, 0))

    expect(members.value).toHaveLength(1)
    expect(maxRounds.value).toBe(4)
  })

  it('defaults maxRounds to 10 when the response omits it', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => ({
      ok: true,
      json: async () => ({ members: [] }),
    })))

    const sid = ref('group-2')
    const { maxRounds } = useGroupMembers(sid)
    await nextTick()
    await new Promise(r => setTimeout(r, 0))

    expect(maxRounds.value).toBe(10)
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
