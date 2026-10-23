import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { nextTick } from 'vue'
import { useSessionSidebar, _resetForTest, SIDEBAR_MIN_WIDTH, SIDEBAR_MAX_WIDTH } from '@/composables/useSessionSidebar'
import {
  PANEL_KEY_PREFIX,
  _resetLayoutScopeForTest,
  setLayoutProjectRoot,
  setLayoutScopeMode,
} from '@/utils/panelState'

const KEY = PANEL_KEY_PREFIX + 'session-sidebar'
const ROOT_A = '/project/a'

describe('useSessionSidebar', () => {
  beforeEach(() => {
    _resetForTest()
    _resetLayoutScopeForTest()
    localStorage.clear()
  })
  afterEach(() => _resetLayoutScopeForTest())

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

  it('openSessionTabBridge opens the drawer without unpinning the sidebar', () => {
    const s = useSessionSidebar()
    const openDrawer = vi.fn()
    s.registerOpenDrawer(openDrawer)
    // Sidebar pinned → bridge keeps it pinned AND opens the drawer
    s.open.value = true
    s.openSessionTabBridge()
    expect(s.open.value).toBe(true)
    expect(openDrawer).toHaveBeenCalled()
    // Sidebar closed → bridge just opens drawer
    openDrawer.mockClear()
    s.closeSidebar()
    s.openSessionTabBridge()
    expect(s.open.value).toBe(false)
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

describe('useSessionSidebar — layout memory scope', () => {
  beforeEach(() => {
    _resetForTest()
    _resetLayoutScopeForTest()
    localStorage.clear()
  })
  afterEach(() => _resetLayoutScopeForTest())

  it('re-reads the stored state when the scope switches, without a reload', async () => {
    // The whole point of the layout-memory switch: it must take effect in place.
    // This module loads once and guards with `initialized`, so a missing epoch
    // watcher leaves the old value on screen and the setting looks broken.
    localStorage.setItem(KEY, JSON.stringify({ open: false, width: 320 }))
    const s = useSessionSidebar()
    expect(s.width.value).toBe(320)

    // Project A has its own, wider sidebar.
    localStorage.setItem(KEY + ':' + ROOT_A, JSON.stringify({ open: true, width: 460 }))
    setLayoutProjectRoot(ROOT_A)
    setLayoutScopeMode('project')
    await nextTick()

    expect(s.width.value).toBe(460)
    expect(s.open.value).toBe(true)
  })

  it('writes to the project key once the scope is per-project', async () => {
    const s = useSessionSidebar()
    setLayoutProjectRoot(ROOT_A)
    setLayoutScopeMode('project')
    await nextTick()

    s.setWidth(400)
    expect(localStorage.getItem(KEY + ':' + ROOT_A)).toBe(JSON.stringify({ open: true, width: 400 }))
    // The global record is left alone — it is the seed for other projects.
    expect(localStorage.getItem(KEY)).toBeNull()
  })

  it('falls back to defaults when the scope is per-project and no root is known', async () => {
    localStorage.setItem(KEY, JSON.stringify({ open: false, width: 320 }))
    const s = useSessionSidebar()
    expect(s.width.value).toBe(320)

    setLayoutScopeMode('project')
    await nextTick()

    // No root yet → no physical key → registry default, not the global value.
    expect(s.width.value).toBe(280)
    expect(s.open.value).toBe(true)
  })
})
