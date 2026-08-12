import { describe, it, expect, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { useChatRecommendation } from '../useChatRecommendation'

function setup(overrides: Partial<{ fetchRemote: ReturnType<typeof vi.fn> }> = {}) {
  const activeId = ref('')
  const loading = ref(false)
  const lastAssistant = ref(true)
  const fetchRemote = overrides.fetchRemote ?? vi.fn(async (id: string) => `${id}-rec`)
  const rec = useChatRecommendation({
    activeSessionId: () => activeId.value || undefined,
    loading: () => loading.value,
    isLastMessageAssistant: () => lastAssistant.value,
    fetchRemote,
  })
  return { rec, activeId, loading, lastAssistant, fetchRemote }
}

describe('useChatRecommendation', () => {
  it('starts empty when no active session or no cached value', () => {
    const { rec } = setup()
    expect(rec.current.value).toBe('')
    expect(rec.show.value).toBe(false)
  })

  it('shows a recommendation for the active session via a live event', () => {
    const { rec, activeId } = setup()
    activeId.value = 'A'
    rec.upsert('A', ' 继续实现  ')
    expect(rec.current.value).toBe('继续实现')
    expect(rec.show.value).toBe(true)
  })

  it('ignores a live event from a background session (does not affect active display)', () => {
    const { rec, activeId } = setup()
    activeId.value = 'A'
    rec.upsert('B', 'B 的推荐')
    expect(rec.current.value).toBe('')
    expect(rec.show.value).toBe(false)
    // A real event for the active session is still shown.
    rec.upsert('A', 'A 的推荐')
    expect(rec.current.value).toBe('A 的推荐')
    expect(rec.show.value).toBe(true)
  })

  it('restores a cached value immediately on session switch without fetching', async () => {
    const { rec, activeId, fetchRemote } = setup()
    rec.upsert('A', 'A 的推荐')
    activeId.value = 'A'
    // Switch away then back — cache is reused, no network fetch.
    activeId.value = 'B'
    expect(rec.current.value).toBe('')
    activeId.value = 'A'
    expect(rec.current.value).toBe('A 的推荐')
    expect(rec.show.value).toBe(true)
    expect(fetchRemote).not.toHaveBeenCalled()
  })

  it('fetches the persisted recommendation for a session not yet cached', async () => {
    const { rec, activeId, fetchRemote } = setup()
    activeId.value = 'B'
    await rec.ensureFetched('B')
    expect(fetchRemote).toHaveBeenCalledWith('B')
    expect(rec.current.value).toBe('B-rec')
    expect(rec.show.value).toBe(true)
  })

  it('does not refetch a session that is already cached', async () => {
    const { rec, activeId, fetchRemote } = setup()
    activeId.value = 'A'
    rec.upsert('A', '已缓存')
    await rec.ensureFetched('A')
    expect(fetchRemote).not.toHaveBeenCalled()
  })

  it('does not apply an in-flight fetch that was invalidated mid-flight (generation guard)', async () => {
    let resolveFn!: (v: string) => void
    const { rec, activeId } = setup({ fetchRemote: vi.fn(() => new Promise<string>((res) => { resolveFn = res })) })
    activeId.value = 'B'
    const pending = rec.ensureFetched('B')
    rec.invalidate('B')
    resolveFn('过期值')
    await pending
    await nextTick()
    expect(rec.current.value).toBe('')
    expect(rec.show.value).toBe(false)
  })

  it('hides the recommendation while the session is streaming', () => {
    const { rec, activeId, loading } = setup()
    activeId.value = 'A'
    rec.upsert('A', '推荐')
    expect(rec.show.value).toBe(true)
    loading.value = true
    expect(rec.show.value).toBe(false)
  })

  it('invalidates the active session when streaming starts (clears stale value)', () => {
    const { rec, activeId } = setup()
    activeId.value = 'A'
    rec.upsert('A', '推荐')
    rec.invalidate('A')
    expect(rec.current.value).toBe('')
    expect(rec.show.value).toBe(false)
  })

  it('does not show when the last message is not an assistant reply', () => {
    const { rec, activeId, lastAssistant } = setup()
    activeId.value = 'A'
    rec.upsert('A', '推荐')
    lastAssistant.value = false
    expect(rec.show.value).toBe(false)
    expect(rec.current.value).toBe('推荐')
  })

  it('dismiss hides, and accept returns the text and hides', () => {
    const { rec, activeId } = setup()
    activeId.value = 'A'
    rec.upsert('A', '采纳我')
    rec.dismiss()
    expect(rec.show.value).toBe(false)
    expect(rec.accept()).toBe('')
    // A fresh event resets dismissal.
    rec.upsert('A', '采纳我')
    expect(rec.accept()).toBe('采纳我')
    expect(rec.show.value).toBe(false)
  })

  it('accept returns empty when nothing is available', () => {
    const { rec } = setup()
    expect(rec.accept()).toBe('')
  })

  it('does not throw when the fetch fails', async () => {
    const { rec, activeId, fetchRemote } = setup({ fetchRemote: vi.fn().mockRejectedValue(new Error('network')) })
    activeId.value = 'B'
    await expect(rec.ensureFetched('B')).resolves.toBeUndefined()
    expect(rec.current.value).toBe('')
    expect(rec.show.value).toBe(false)
  })

  it('clear resets all sessions', () => {
    const { rec, activeId } = setup()
    activeId.value = 'A'
    rec.upsert('A', '推荐')
    rec.clear()
    expect(rec.current.value).toBe('')
    expect(rec.show.value).toBe(false)
  })
})
