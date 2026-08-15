import { describe, expect, it, beforeEach, vi } from 'vitest'
import { useSessionSidebar, _resetForTest, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH } from '@/composables/useSessionSidebar'

const KEY = 'clawbench-session-sidebar'

describe('useSessionSidebar', () => {
  beforeEach(() => {
    _resetForTest()
    localStorage.clear()
  })

  it('defaults to open on wide screen with default width when no stored state', () => {
    const s = useSessionSidebar()
    expect(s.open.value).toBe(true)
    expect(s.width.value).toBe(280)
  })

  it('restores stored open state and width', () => {
    localStorage.setItem(KEY, JSON.stringify({ open: false, width: 340 }))
    const s = useSessionSidebar()
    expect(s.open.value).toBe(false)
    expect(s.width.value).toBe(340)
  })

  it('falls back to defaults when localStorage is corrupted', () => {
    localStorage.setItem(KEY, '{not-json')
    const s = useSessionSidebar()
    expect(s.open.value).toBe(true)
    expect(s.width.value).toBe(280)
  })

  it('clamps width to [MIN, MAX]', () => {
    const s = useSessionSidebar()
    s.setWidth(10)
    expect(s.width.value).toBe(SIDEBAR_MIN_WIDTH)
    s.setWidth(9000)
    expect(s.width.value).toBe(SIDEBAR_MAX_WIDTH)
  })

  it('setWidth persists to localStorage', () => {
    const s = useSessionSidebar()
    s.setWidth(300)
    const stored = JSON.parse(localStorage.getItem(KEY) || '{}')
    expect(stored.width).toBe(300)
  })

  it('openSidebar/closeSidebar persist state', () => {
    const s = useSessionSidebar()
    s.openSidebar()
    expect(localStorage.getItem(KEY)).toContain('"open":true')
    s.closeSidebar()
    expect(localStorage.getItem(KEY)).toContain('"open":false')
  })

  it('pinToSidebar opens sidebar', () => {
    const s = useSessionSidebar()
    s.closeSidebar()
    s.pinToSidebar()
    expect(s.open.value).toBe(true)
  })

  it('unpinToDrawer just collapses the sidebar (does NOT open the drawer)', () => {
    const s = useSessionSidebar()
    const openDrawer = vi.fn()
    s.registerOpenDrawer(openDrawer)
    s.pinToSidebar()
    s.unpinToDrawer()
    expect(s.open.value).toBe(false)
    expect(openDrawer).not.toHaveBeenCalled()
  })

  it('openSessionTabBridge switches pinned sidebar to drawer mode and opens it', () => {
    const s = useSessionSidebar()
    const openDrawer = vi.fn()
    s.registerOpenDrawer(openDrawer)
    // Sidebar pinned → bridge closes it AND opens the drawer
    s.open.value = true
    s.openSessionTabBridge()
    expect(s.open.value).toBe(false)
    expect(openDrawer).toHaveBeenCalled()
    // Sidebar closed → bridge opens drawer
    openDrawer.mockClear()
    s.openSessionTabBridge()
    expect(openDrawer).toHaveBeenCalled()
  })

  it('delegates addSessionLocally to registered callback', () => {
    const s = useSessionSidebar()
    const fn = vi.fn()
    s.registerAddSessionLocally(fn)
    s.addSessionLocally({ id: 'x' })
    expect(fn).toHaveBeenCalledWith({ id: 'x' })
  })
})
