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

  it('syncIndexFromScroll picks the last target at or above the position', () => {
    const targets = ref<number[]>([100, 300, 500])
    const nav = useChangeNav(targets, vi.fn())
    nav.syncIndexFromScroll(350)
    expect(nav.index.value).toBe(1)
    nav.syncIndexFromScroll(50)
    expect(nav.index.value).toBe(0)
    nav.syncIndexFromScroll(9999)
    expect(nav.index.value).toBe(2)
  })
})
