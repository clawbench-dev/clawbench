import { describe, it, expect, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { useChatRecommendation } from '../useChatRecommendation'

function setup(overrides: Partial<{ fetchRemote: ReturnType<typeof vi.fn> }> = {}) {
  const activeId = ref('')
  const loading = ref(false)
  const lastAssistant = ref(true)
  const lastMsgId = ref<number | string | undefined>(undefined)
  const fetchRemote = overrides.fetchRemote ?? vi.fn(async (id: string, mid: number | string) => `${id}-${mid}-rec`)
  const rec = useChatRecommendation({
    activeSessionId: () => activeId.value || undefined,
    loading: () => loading.value,
    isLastMessageAssistant: () => lastAssistant.value,
    lastAssistantMessageId: () => lastMsgId.value,
    fetchRemote,
  })
  return { rec, activeId, loading, lastAssistant, lastMsgId, fetchRemote }
}

describe('useChatRecommendation', () => {
  it('starts empty when no active session or no cached value', () => {
    const { rec } = setup()
    expect(rec.current.value).toBe('')
    expect(rec.show.value).toBe(false)
  })

  it('shows a recommendation for the active session via a live event', () => {
    const { rec, activeId, lastMsgId } = setup()
    activeId.value = 'A'
    lastMsgId.value = 101
    rec.upsert('A', ' 继续实现  ', 101)
    expect(rec.current.value).toBe('继续实现')
    expect(rec.show.value).toBe(true)
  })

  it('ignores a live event from a background session (does not affect active display)', () => {
    const { rec, activeId, lastMsgId } = setup()
    activeId.value = 'A'
    lastMsgId.value = 101
    rec.upsert('B', 'B 的推荐', 99)
    expect(rec.current.value).toBe('')
    expect(rec.show.value).toBe(false)
    // A real event for the active session is still shown.
    rec.upsert('A', 'A 的推荐', 101)
    expect(rec.current.value).toBe('A 的推荐')
    expect(rec.show.value).toBe(true)
  })

  it('hides a stale recommendation whose message id does not match the last assistant message', () => {
    const { rec, activeId, lastMsgId } = setup()
    activeId.value = 'A'
    // Last assistant message is 102, but the cached recommendation belongs to 101.
    lastMsgId.value = 102
    rec.upsert('A', '旧推荐', 101)
    expect(rec.current.value).toBe('')
    expect(rec.show.value).toBe(false)
    // Once the last assistant message becomes the one the recommendation was
    // generated for, it is shown again.
    lastMsgId.value = 101
    expect(rec.current.value).toBe('旧推荐')
    expect(rec.show.value).toBe(true)
  })

  it('does not show a recommendation when there is no last assistant message id', () => {
    const { rec, activeId, lastMsgId } = setup()
    activeId.value = 'A'
    lastMsgId.value = undefined
    rec.upsert('A', '推荐', 101)
    expect(rec.current.value).toBe('')
    expect(rec.show.value).toBe(false)
  })

  it('restores a cached value immediately on session switch without fetching', async () => {
    const { rec, activeId, lastMsgId, fetchRemote } = setup()
    rec.upsert('A', 'A 的推荐', 101)
    activeId.value = 'A'
    lastMsgId.value = 101
    // Switch away then back — cache is reused, no network fetch.
    activeId.value = 'B'
    expect(rec.current.value).toBe('')
    activeId.value = 'A'
    lastMsgId.value = 101
    expect(rec.current.value).toBe('A 的推荐')
    expect(rec.show.value).toBe(true)
    expect(fetchRemote).not.toHaveBeenCalled()
  })

  it('fetches the persisted recommendation for a session+message not yet cached', async () => {
    const { rec, activeId, lastMsgId, fetchRemote } = setup()
    activeId.value = 'B'
    lastMsgId.value = 202
    await rec.ensureFetched('B', 202)
    expect(fetchRemote).toHaveBeenCalledWith('B', 202)
    expect(rec.current.value).toBe('B-202-rec')
    expect(rec.show.value).toBe(true)
  })

  it('does not refetch a session that is already cached', async () => {
    const { rec, activeId, lastMsgId, fetchRemote } = setup()
    activeId.value = 'A'
    lastMsgId.value = 101
    rec.upsert('A', '已缓存', 101)
    await rec.ensureFetched('A', 101)
    expect(fetchRemote).not.toHaveBeenCalled()
  })

  it('does not apply an in-flight fetch that was invalidated mid-flight (generation guard)', async () => {
    let resolveFn!: (v: string) => void
    const { rec, activeId, lastMsgId } = setup({ fetchRemote: vi.fn(() => new Promise<string>((res) => { resolveFn = res })) })
    activeId.value = 'B'
    lastMsgId.value = 202
    const pending = rec.ensureFetched('B', 202)
    rec.invalidate('B')
    resolveFn('过期值')
    await pending
    await nextTick()
    expect(rec.current.value).toBe('')
    expect(rec.show.value).toBe(false)
  })

  it('hides the recommendation while the session is streaming', () => {
    const { rec, activeId, lastMsgId, loading } = setup()
    activeId.value = 'A'
    lastMsgId.value = 101
    rec.upsert('A', '推荐', 101)
    expect(rec.show.value).toBe(true)
    loading.value = true
    expect(rec.show.value).toBe(false)
  })

  it('invalidates the active session when streaming starts (clears stale value)', () => {
    const { rec, activeId, lastMsgId } = setup()
    activeId.value = 'A'
    lastMsgId.value = 101
    rec.upsert('A', '推荐', 101)
    rec.invalidate('A')
    expect(rec.current.value).toBe('')
    expect(rec.show.value).toBe(false)
  })

  it('does not show when the last message is not an assistant reply', () => {
    const { rec, activeId, lastMsgId, lastAssistant } = setup()
    activeId.value = 'A'
    lastMsgId.value = 101
    rec.upsert('A', '推荐', 101)
    lastAssistant.value = false
    expect(rec.show.value).toBe(false)
    expect(rec.current.value).toBe('推荐')
  })

  it('dismiss hides, and accept returns the text and hides', () => {
    const { rec, activeId, lastMsgId } = setup()
    activeId.value = 'A'
    lastMsgId.value = 101
    rec.upsert('A', '采纳我', 101)
    rec.dismiss()
    expect(rec.show.value).toBe(false)
    expect(rec.accept()).toBe('')
    // A fresh event resets dismissal.
    rec.upsert('A', '采纳我', 101)
    expect(rec.accept()).toBe('采纳我')
    expect(rec.show.value).toBe(false)
  })

  it('accept returns empty when the recommendation is stale (message id mismatch)', () => {
    const { rec, activeId, lastMsgId } = setup()
    activeId.value = 'A'
    lastMsgId.value = 102
    rec.upsert('A', '旧推荐', 101)
    expect(rec.accept()).toBe('')
    expect(rec.current.value).toBe('')
  })

  it('accept returns empty when nothing is available', () => {
    const { rec } = setup()
    expect(rec.accept()).toBe('')
  })

  it('does not throw when the fetch fails', async () => {
    const { rec, activeId, lastMsgId, fetchRemote } = setup({ fetchRemote: vi.fn().mockRejectedValue(new Error('network')) })
    activeId.value = 'B'
    lastMsgId.value = 202
    await expect(rec.ensureFetched('B', 202)).resolves.toBeUndefined()
    expect(rec.current.value).toBe('')
    expect(rec.show.value).toBe(false)
  })

  it('clear resets all sessions', () => {
    const { rec, activeId, lastMsgId } = setup()
    activeId.value = 'A'
    lastMsgId.value = 101
    rec.upsert('A', '推荐', 101)
    rec.clear()
    expect(rec.current.value).toBe('')
    expect(rec.show.value).toBe(false)
  })
})
