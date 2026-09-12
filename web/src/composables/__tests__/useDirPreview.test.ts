import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { useDirPreview } from '@/composables/useDirPreview'

const mockApiGet = vi.fn()

vi.mock('@/utils/api', () => ({
  apiGet: (...args: unknown[]) => mockApiGet(...args),
}))
vi.mock('@/utils/appLog', () => ({
  appLog: { d: vi.fn(), i: vi.fn(), w: vi.fn(), e: vi.fn() },
}))

function mount(initial: { active?: boolean; path?: string; showHidden?: boolean } = {}) {
  const active = ref(initial.active ?? true)
  const dirPath = ref(initial.path ?? 'src')
  const showHidden = ref(initial.showHidden ?? false)
  const api = useDirPreview({ active, dirPath, showHidden })
  return { active, dirPath, showHidden, ...api }
}

describe('useDirPreview', () => {
  beforeEach(() => {
    mockApiGet.mockReset()
    mockApiGet.mockResolvedValue({ items: [{ name: 'a.ts', type: 'file' }] })
  })

  it('lists the directory via /api/dir when active', async () => {
    const d = mount()
    await nextTick()
    await vi.waitFor(() => expect(d.loadedPath.value).toBe('src'))

    expect(mockApiGet).toHaveBeenCalledWith('/api/dir?path=src', expect.objectContaining({ timeoutMs: 10_000 }))
    expect(d.entries.value).toEqual([{ name: 'a.ts', type: 'file' }])
    expect(d.loading.value).toBe(false)
  })

  it('encodes the path so nested directories survive', async () => {
    const d = mount({ path: 'src/components/deep dir' })
    await nextTick()
    await vi.waitFor(() => expect(d.loadedPath.value).toBe('src/components/deep dir'))

    expect(mockApiGet).toHaveBeenCalledWith(
      `/api/dir?path=${encodeURIComponent('src/components/deep dir')}`,
      expect.anything(),
    )
  })

  it('does not fetch while inactive', async () => {
    const d = mount({ active: false })
    await nextTick()
    expect(mockApiGet).not.toHaveBeenCalled()
    expect(d.entries.value).toEqual([])
  })

  it('releases the listing when it goes inactive so a stale dir cannot flash', async () => {
    const d = mount()
    await nextTick()
    await vi.waitFor(() => expect(d.entries.value.length).toBe(1))

    d.active.value = false
    await nextTick()
    expect(d.entries.value).toEqual([])
    expect(d.loadedPath.value).toBe('')
  })

  it('re-fetches when the directory changes', async () => {
    const d = mount()
    await nextTick()
    await vi.waitFor(() => expect(d.loadedPath.value).toBe('src'))

    d.dirPath.value = 'docs'
    await nextTick()
    await vi.waitFor(() => expect(d.loadedPath.value).toBe('docs'))
    expect(mockApiGet).toHaveBeenCalledTimes(2)
  })

  it('drops a slow response for a previous directory (out-of-order guard)', async () => {
    const resolvers: Array<(v: unknown) => void> = []
    mockApiGet.mockImplementation(() => new Promise(res => { resolvers.push(res) }))

    const d = mount()
    await nextTick()
    d.dirPath.value = 'docs'
    await nextTick()

    // Resolve the SECOND request first, then the stale first one.
    resolvers[1]({ items: [{ name: 'new.md', type: 'file' }] })
    await vi.waitFor(() => expect(d.entries.value).toEqual([{ name: 'new.md', type: 'file' }]))
    resolvers[0]({ items: [{ name: 'stale.ts', type: 'file' }] })
    await nextTick()

    // The stale listing must not overwrite the current one.
    expect(d.entries.value).toEqual([{ name: 'new.md', type: 'file' }])
  })

  it('flags an error and clears entries on failure', async () => {
    mockApiGet.mockRejectedValue(new Error('boom'))
    const d = mount()
    await nextTick()
    await vi.waitFor(() => expect(d.error.value).toBe(true))

    expect(d.entries.value).toEqual([])
    expect(d.loading.value).toBe(false)
  })

  it('hides dotfiles unless showHidden is on', async () => {
    const d = mount()
    await nextTick()
    expect(d.visible({ name: '.env', type: 'file' })).toBe(false)
    expect(d.visible({ name: 'index.ts', type: 'file' })).toBe(true)

    d.showHidden.value = true
    expect(d.visible({ name: '.env', type: 'file' })).toBe(true)
  })

  it('refresh re-lists the current directory', async () => {
    const d = mount()
    await nextTick()
    await vi.waitFor(() => expect(d.loadedPath.value).toBe('src'))

    d.refresh()
    await vi.waitFor(() => expect(mockApiGet).toHaveBeenCalledTimes(2))
  })

  it('refresh is a no-op when inactive', async () => {
    const d = mount({ active: false })
    await nextTick()
    d.refresh()
    await nextTick()
    expect(mockApiGet).not.toHaveBeenCalled()
  })
})
