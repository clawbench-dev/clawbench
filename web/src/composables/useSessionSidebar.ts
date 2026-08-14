import { ref } from 'vue'

export const SESSION_SIDEBAR_KEY = 'clawbench-session-sidebar'
export const SIDEBAR_DEFAULT_WIDTH = 280
export const SIDEBAR_MIN_WIDTH = 220
export const SIDEBAR_MAX_WIDTH = 480
export const SIDEBAR_DEFAULT_HEIGHT = 320
export const SIDEBAR_MIN_HEIGHT = 160
export const SIDEBAR_MAX_HEIGHT = 600

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
const height = ref(SIDEBAR_DEFAULT_HEIGHT)
let openDrawerFn: (() => void) | null = null
let addLocallyFn: ((session: SidebarSession) => void) | null = null
let initialized = false

function clampWidth(w: number): number {
  if (!Number.isFinite(w)) return SIDEBAR_DEFAULT_WIDTH
  return Math.max(SIDEBAR_MIN_WIDTH, Math.min(SIDEBAR_MAX_WIDTH, Math.round(w)))
}

function clampHeight(h: number): number {
  if (!Number.isFinite(h)) return SIDEBAR_DEFAULT_HEIGHT
  return Math.max(SIDEBAR_MIN_HEIGHT, Math.min(SIDEBAR_MAX_HEIGHT, Math.round(h)))
}

function load() {
  try {
    const raw = localStorage.getItem(SESSION_SIDEBAR_KEY)
    if (!raw) return
    const parsed = JSON.parse(raw)
    if (parsed && typeof parsed === 'object') {
      open.value = parsed.open === true
      width.value = clampWidth(Number(parsed.width) || SIDEBAR_DEFAULT_WIDTH)
      height.value = clampHeight(Number(parsed.height) || SIDEBAR_DEFAULT_HEIGHT)
    }
  } catch {
    // corrupted storage → keep defaults
  }
}

function persist() {
  try {
    localStorage.setItem(SESSION_SIDEBAR_KEY, JSON.stringify({ open: open.value, width: width.value, height: height.value }))
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
  function setHeight(h: number) {
    height.value = clampHeight(h)
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
  /** Bridge for openSessionTab: sidebar open → collapse it; else open the drawer. */
  function openSessionTabBridge() {
    if (open.value) {
      closeSidebar()
    } else {
      openDrawerFn?.()
    }
  }

  return {
    open,
    width,
    height,
    openSidebar,
    closeSidebar,
    toggleSidebar,
    setWidth,
    setHeight,
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
  height.value = SIDEBAR_DEFAULT_HEIGHT
  openDrawerFn = null
  addLocallyFn = null
}
