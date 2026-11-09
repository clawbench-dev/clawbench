/**
 * v-dock-tip directive — the wide-screen dock's icon label.
 *
 * The vertical dock is a 48dp rail of icon-only buttons, so the tab name has to
 * come from somewhere else. `title` is not enough: it waits ~1s, the OS draws it
 * in its own style, and Android WebView never renders it at all (there is no
 * hover) — which is exactly the tablet-in-landscape case this dock shows up in.
 * So the label is ours: one themed chip, teleported to <body> because the dock's
 * scrolling column (`overflow-y: auto` clips both axes) would cut it off.
 *
 * Three ways in, one chip:
 * - pointer hover, after HOVER_DELAY_MS (immediately would flash on every pass)
 * - keyboard focus, immediately (Tab is already deliberate)
 * - touch: 450ms hold, the same gesture and the same semantics as v-long-press —
 *   the touchend is default-prevented so releasing after a hold does NOT also
 *   click the tab. The dock's click switches or collapses panes, and a gesture
 *   that exists to reveal a name must not navigate.
 *
 * Usage: <button v-dock-tip="dockTabTitle(tab)">
 */

import type { DirectiveBinding } from 'vue'

const HOVER_DELAY_MS = 300
const LONG_PRESS_MS = 450
const MOVE_THRESHOLD_PX = 10
/** Gap between the button and the chip, in design px (see `--dp`). */
const GAP_DP = 8
const VIEWPORT_MARGIN_PX = 4
const VISIBLE_CLASS = 'is-visible'

interface TipState {
  text: string
  hoverTimer: number | null
  pressTimer: number | null
  /** A long press was recognised: the next click must not reach the button. */
  fired: boolean
  startX: number
  startY: number
}

// One chip for the whole dock. Rendering one per button would put a dozen
// hidden nodes in the DOM and still need the same teleport.
let chipEl: HTMLDivElement | null = null
/** The button the chip currently describes (null while hidden). */
let activeEl: HTMLElement | null = null
let watchedViewport = false

function chip(): HTMLDivElement {
  if (chipEl && chipEl.isConnected) return chipEl
  chipEl = document.createElement('div')
  chipEl.className = 'dock-tip'
  chipEl.setAttribute('role', 'tooltip')
  document.body.appendChild(chipEl)
  if (!watchedViewport) {
    watchedViewport = true
    // The chip is positioned from a rect measured once. Any scroll (the dock
    // itself scrolls) or resize invalidates it, so drop it rather than leave it
    // pointing at where the button used to be.
    window.addEventListener('scroll', hide, true)
    window.addEventListener('resize', hide)
  }
  return chipEl
}

/** One design pixel in CSS px — the root font size is 15px * UI scale. */
function dp(): number {
  const root = parseFloat(getComputedStyle(document.documentElement).fontSize)
  return Number.isFinite(root) && root > 0 ? root / 15 : 1
}

function show(el: HTMLElement, text: string): void {
  if (!text) return
  const tip = chip()
  activeEl = el
  tip.textContent = text
  tip.classList.add(VISIBLE_CLASS)

  const rect = el.getBoundingClientRect()
  const gap = GAP_DP * dp()
  let left = rect.right + gap
  let top = rect.top + rect.height / 2 - tip.offsetHeight / 2

  // Flip to the inside of the rail when the dock sits against the right edge
  // (it never does today, but a clipped label is worse than a mirrored one).
  if (left + tip.offsetWidth > window.innerWidth - VIEWPORT_MARGIN_PX) {
    left = rect.left - gap - tip.offsetWidth
  }
  left = Math.max(VIEWPORT_MARGIN_PX, left)
  top = Math.max(
    VIEWPORT_MARGIN_PX,
    Math.min(top, window.innerHeight - tip.offsetHeight - VIEWPORT_MARGIN_PX),
  )

  tip.style.left = `${Math.round(left)}px`
  tip.style.top = `${Math.round(top)}px`
}

function hide(): void {
  activeEl = null
  chipEl?.classList.remove(VISIBLE_CLASS)
}

function hoverCapable(): boolean {
  return typeof window !== 'undefined'
    && typeof window.matchMedia === 'function'
    && window.matchMedia('(hover: hover)').matches
}

function mounted(el: HTMLElement, binding: DirectiveBinding<string>): void {
  const state: TipState = {
    text: binding.value ?? '',
    hoverTimer: null,
    pressTimer: null,
    fired: false,
    startX: 0,
    startY: 0,
  }

  function clearHover() {
    if (state.hoverTimer !== null) {
      clearTimeout(state.hoverTimer)
      state.hoverTimer = null
    }
  }

  function clearPress() {
    if (state.pressTimer !== null) {
      clearTimeout(state.pressTimer)
      state.pressTimer = null
    }
  }

  function onEnter() {
    if (!hoverCapable()) return
    clearHover()
    state.hoverTimer = window.setTimeout(() => {
      state.hoverTimer = null
      show(el, state.text)
    }, HOVER_DELAY_MS)
  }

  function onLeave() {
    clearHover()
    hide()
  }

  function onFocus() {
    // Touch taps focus the button too; only a real pointer gets an instant chip.
    if (!hoverCapable()) return
    clearHover()
    show(el, state.text)
  }

  function onBlur() {
    hide()
  }

  function onClick() {
    hide()
  }

  function onTouchStart(e: TouchEvent) {
    state.fired = false
    const touch = e.touches[0]
    if (!touch) return
    state.startX = touch.clientX
    state.startY = touch.clientY
    clearPress()
    state.pressTimer = window.setTimeout(() => {
      state.pressTimer = null
      state.fired = true
      show(el, state.text)
    }, LONG_PRESS_MS)
  }

  function onTouchMove(e: TouchEvent) {
    if (state.pressTimer === null) return
    const touch = e.touches[0]
    if (!touch) return
    if (
      Math.abs(touch.clientX - state.startX) > MOVE_THRESHOLD_PX
      || Math.abs(touch.clientY - state.startY) > MOVE_THRESHOLD_PX
    ) {
      clearPress()
      hide()
    }
  }

  function onTouchEnd(e: TouchEvent) {
    const held = state.pressTimer === null && state.fired
    clearPress()
    if (held) {
      // Swallow the synthetic click the release would otherwise produce.
      e.preventDefault()
      hide()
    }
    state.fired = false
  }

  function onTouchCancel() {
    clearPress()
    state.fired = false
    hide()
  }

  function onContextMenu(e: Event) {
    // Android/iOS synthesise a contextmenu ~500ms into a hold; the chip is the
    // only thing this gesture should produce.
    if (state.fired) e.preventDefault()
  }

  el.addEventListener('mouseenter', onEnter)
  el.addEventListener('mouseleave', onLeave)
  el.addEventListener('focus', onFocus)
  el.addEventListener('blur', onBlur)
  el.addEventListener('click', onClick)
  el.addEventListener('touchstart', onTouchStart, { passive: true })
  el.addEventListener('touchmove', onTouchMove, { passive: true })
  el.addEventListener('touchend', onTouchEnd, { passive: false })
  el.addEventListener('touchcancel', onTouchCancel, { passive: true })
  el.addEventListener('contextmenu', onContextMenu, { passive: false })

  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ;(el as any)._dockTip_state = state
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ;(el as any)._dockTip_cleanup = () => {
    clearHover()
    clearPress()
    el.removeEventListener('mouseenter', onEnter)
    el.removeEventListener('mouseleave', onLeave)
    el.removeEventListener('focus', onFocus)
    el.removeEventListener('blur', onBlur)
    el.removeEventListener('click', onClick)
    el.removeEventListener('touchstart', onTouchStart)
    el.removeEventListener('touchmove', onTouchMove)
    el.removeEventListener('touchend', onTouchEnd)
    el.removeEventListener('touchcancel', onTouchCancel)
    el.removeEventListener('contextmenu', onContextMenu)
    hide()
  }
}

function updated(el: HTMLElement, binding: DirectiveBinding<string>): void {
  // Language switch / forge platform change: the label of a chip that is
  // already on screen has to follow, not wait for the next hover. `updated`
  // fires for every button in the v-for, so only the one the chip describes
  // may rewrite it.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  const state = (el as any)._dockTip_state as TipState | undefined
  if (!state) return
  state.text = binding.value ?? ''
  if (activeEl === el && chipEl?.classList.contains(VISIBLE_CLASS)) {
    chipEl.textContent = state.text
  }
}

function unmounted(el: HTMLElement): void {
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  ;(el as any)._dockTip_cleanup?.()
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (el as any)._dockTip_cleanup
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  delete (el as any)._dockTip_state
}

export const DockTipDirective = { mounted, updated, unmounted }
