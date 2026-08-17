import { ref } from 'vue'

export const SESSION_SIDEBAR_KEY = 'clawbench-session-sidebar'
export const SIDEBAR_DEFAULT_WIDTH = 280
export const SIDEBAR_MIN_WIDTH = 220
export const SIDEBAR_MAX_WIDTH = 480

export interface SidebarSession {
  id: string
  title?: string
  backend?: string
  agentId?: string
  model?: string
  updatedAt?: string
  unreadCount?: number
}

const open = ref(true)
const width = ref(SIDEBAR_DEFAULT_WIDTH)
let openDrawerFn: (() => void) | null = null
let addLocallyFn: ((session: SidebarSession) => void) | null = null
let initialized = false

function clampWidth(w: number): number {
  if (!Number.isFinite(w)) return SIDEBAR_DEFAULT_WIDTH
  return Math.max(SIDEBAR_MIN_WIDTH, Math.min(SIDEBAR_MAX_WIDTH, Math.round(w)))
}

function load() {
  try {
    const raw = localStorage.getItem(SESSION_SIDEBAR_KEY)
    if (!raw) return
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed === 'object') {
      open.value = parsed.open === true
      width.value = clampWidth(Number(parsed.width) || SIDEBAR_DEFAULT_WIDTH)
    }
  } catch {
    // corrupted storage → keep defaults
  }
}

function persist() {
  try {
    localStorage.setItem(SESSION_SIDEBAR_KEY, JSON.stringify({ open: open.value, width: width.value }))
  } catch {
    // ignore
  }
}

export function useSessionSidebar() {
  if (!initialized) {
    initialized = true
    load()
  }

  function openSidebar() {
    open.value = true
    persist()
  }
  function closeSidebar() {
    open.value = false
    persist()
  }
  function toggleSidebar() {
    if (open.value) {
      closeSidebar()
    } else {
      openSidebar()
    }
  }
  function setWidth(w: number) {
    width.value = clampWidth(w)
    persist()
  }
  function pinToSidebar() {
    openSidebar()
  }
  /** Unpin / deselect: just collapse the sidebar (does NOT re-open the drawer). */
  function unpinToDrawer() {
    closeSidebar()
  }
  function registerOpenDrawer(fn: () => void) {
    openDrawerFn = fn
  }
  function registerAddSessionLocally(fn: (session: SidebarSession) => void) {
    addLocallyFn = fn
  }
  function addSessionLocally(session: SidebarSession) {
    addLocallyFn?.(session)
  }
  /** Bridge for openSessionTab: keep the sidebar as-is and just open the drawer. */
  function openSessionTabBridge() {
    openDrawerFn?.()
  }

  return {
    open,
    width,
    openSidebar,
    closeSidebar,
    toggleSidebar,
    setWidth,
    pinToSidebar,
    unpinToDrawer,
    registerOpenDrawer,
    registerAddSessionLocally,
    addSessionLocally,
    openSessionTabBridge,
  }
}

/** Test hook — reset module state. */
export function _resetForTest() {
  initialized = false
  open.value = true
  width.value = SIDEBAR_DEFAULT_WIDTH
  openDrawerFn = null
  addLocallyFn = null
}
