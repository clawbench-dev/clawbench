import { describe, it, expect, vi, beforeEach } from 'vitest'
import { ref, nextTick } from 'vue'
import { useChangeNav } from '@/composables/useChangeNav.ts'

describe('useChangeNav', () => {
  beforeEach(() => { vi.clearAllMocks() })

  it('starts at index 0 and reports count from targets', async () => {
    const targets = ref<number[]>([100, 300, 500])
    const nav = useChangeNav(targets, vi.fn())
    expect(nav.count.value).toBe(3)
    expect(nav.index.value).toBe(0)
  })

  it('next advances and calls scrollTo with the new index', () => {
    const scrollTo = vi.fn()
    const targets = ref<number[]>([100, 300, 500])
    const nav = useChangeNav(targets, scrollTo)
    nav.next()
    expect(nav.index.value).toBe(1)
    expect(scrollTo).toHaveBeenCalledWith(1)
  })

  it('next does not advance past the last target', () => {
    const scrollTo = vi.fn()
    const targets = ref<number[]>([100, 300])
    const nav = useChangeNav(targets, scrollTo)
    nav.next()
    nav.next()
    expect(nav.index.value).toBe(1)
    expect(scrollTo).toHaveBeenCalledTimes(1)
  })

  it('prev does not go below 0', () => {
    const scrollTo = vi.fn()
    const targets = ref<number[]>([100, 300])
    const nav = useChangeNav(targets, scrollTo)
    nav.prev()
    expect(nav.index.value).toBe(0)
    expect(scrollTo).not.toHaveBeenCalled()
  })

  it('clamps index when targets shrink', async () => {
    const targets = ref<number[]>([100, 300, 500])
    const nav = useChangeNav(targets, vi.fn())
    nav.next()
    nav.next()
    expect(nav.index.value).toBe(2)
    targets.value = [100]
    await nextTick()
    expect(nav.index.value).toBe(0)
  })

  it('syncIndexFromScroll picks the last target at or below the position', () => {
    const targets = ref<number[]>([100, 300, 500])
    const nav = useChangeNav(targets, vi.fn())
    nav.syncIndexFromScroll(350)
    expect(nav.index.value).toBe(1)
    nav.syncIndexFromScroll(50)
    expect(nav.index.value).toBe(0)
    nav.syncIndexFromScroll(9999)
    expect(nav.index.value).toBe(2)
  })

  it('resets the index to the first change when resetKey changes (new file)', async () => {
    const targets = ref<number[]>([100, 300, 500])
    const filePath = ref('/a.md')
    const nav = useChangeNav(targets, vi.fn(), filePath)
    nav.next()
    nav.next()
    expect(nav.index.value).toBe(2)
    // Switching files must start at the new file's first change, not carry the
    // previous file's position (which would read e.g. "3/4").
    filePath.value = '/b.md'
    await nextTick()
    expect(nav.index.value).toBe(0)
  })

  it('does not reset when only the targets change (same file, new content)', async () => {
    const targets = ref<number[]>([100, 300, 500])
    const filePath = ref('/a.md')
    const nav = useChangeNav(targets, vi.fn(), filePath)
    nav.next()
    expect(nav.index.value).toBe(1)
    targets.value = [100, 300, 500, 700]
    await nextTick()
    expect(nav.index.value).toBe(1)
  })
})
