import { describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import { useListNav } from '@/composables/useListNav'

function setup(count = 5) {
  let n = count
  const confirmed: number[] = []
  const changes: number[] = []
  const nav = useListNav({
    getCount: () => n,
    onConfirm: (i) => confirmed.push(i),
    onActiveChange: (i) => changes.push(i),
  })
  return { nav, confirmed, changes, setCount: (v: number) => { n = v } }
}

describe('useListNav', () => {
  it('starts with no active index', () => {
    const { nav } = setup()
    expect(nav.activeIndex.value).toBe(-1)
  })

  it('ArrowDown from unset selects the first item', () => {
    const { nav, changes } = setup()
    nav.down()
    expect(nav.activeIndex.value).toBe(0)
    expect(changes).toEqual([0])
  })

  it('ArrowUp from unset selects the last item', () => {
    const { nav } = setup(3)
    nav.up()
    expect(nav.activeIndex.value).toBe(2)
  })

  it('ArrowDown wraps to first after the last item', () => {
    const { nav } = setup(2)
    nav.down()
    nav.down()
    expect(nav.activeIndex.value).toBe(1)
    nav.down()
    expect(nav.activeIndex.value).toBe(0)
  })

  it('does not wrap when wrap is false', () => {
    let n = 2
    const nav = useListNav({
      getCount: () => n,
      onConfirm: () => {},
      wrap: false,
    })
    nav.down()
    nav.down()
    expect(nav.activeIndex.value).toBe(1)
    nav.down()
    expect(nav.activeIndex.value).toBe(1)
  })

  it('confirm calls onConfirm with the active index', () => {
    const { nav, confirmed } = setup()
    nav.down()
    nav.down()
    nav.confirm()
    expect(confirmed).toEqual([1])
  })

  it('confirm falls back to the first item when nothing is highlighted', () => {
    const { nav, confirmed } = setup()
    nav.confirm()
    expect(confirmed).toEqual([0])
  })

  it('confirm does nothing on an empty list', () => {
    const { nav, confirmed, setCount } = setup()
    setCount(0)
    nav.confirm()
    expect(confirmed).toEqual([])
  })

  it('down/up do nothing on an empty list', () => {
    const { nav, setCount } = setup()
    setCount(0)
    nav.down()
    expect(nav.activeIndex.value).toBe(-1)
    nav.up()
    expect(nav.activeIndex.value).toBe(-1)
  })

  it('reset clears the highlight', async () => {
    const { nav, setCount } = setup()
    nav.down()
    nav.reset()
    setCount(3)
    await nextTick()
    expect(nav.activeIndex.value).toBe(-1)
  })

  it('setActive moves to a specific index and clamps', () => {
    const { nav, changes } = setup(3)
    nav.setActive(99)
    expect(nav.activeIndex.value).toBe(2)
    expect(changes).toEqual([2])
  })
})
