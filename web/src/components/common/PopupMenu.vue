<template>
  <Teleport to="body">
    <Transition name="menu-fade" @after-leave="emit('after-leave')">
      <div v-if="show" ref="menuEl" class="popup-menu" :class="{ 'popup-menu--app': appSurface }" role="menu" :style="menuStyle" @click.stop="emit('update:show', false)" @keydown.escape="emit('update:show', false)">
        <slot />
      </div>
    </Transition>
  </Teleport>
</template>

<script>
// Module scope (not `<script setup>`, which runs once PER INSTANCE): a counter
// declared in the setup block would reset for every menu and hand them all the
// same id, making the registered handler ids indistinguishable in logs.
let _menuSeq = 0
function nextMenuSeq() {
  return ++_menuSeq
}
</script>

<script setup>
import { ref, watch, onMounted, onBeforeUnmount } from 'vue'
import { computeMenuStyle } from '@/utils/popupMenuPosition'
import { measureFixedContainingBlock } from '@/utils/fixedPosition'
import { registerBackHandler, PRIORITY_OVERLAY } from '@/composables/useBackHandler'

/** Disambiguates several menus that may be open at once (nested pickers). */
const instanceSeq = nextMenuSeq()

const props = defineProps({
  show: Boolean,
  targetElement: { type: Object }, // DOM element reference
  maxWidth: { type: Number, default: 220 },
  maxHeight: { type: Number, default: 320 },
  edgeMargin: { type: Number, default: 6 },
  menuItemsCount: { type: Number, default: 10 }, // for height estimation
  anchor: { type: String, default: 'auto', validator: (v) => ['left', 'right', 'auto'].includes(v) }, // force horizontal alignment
  /**
   * Style the popup like the app-header dropdown (.app-menu): primary
   * background, downward shadow, subtle vertical padding and a flex-column
   * layout with overflow hidden — so a header/footer can stay pinned while the
   * middle list scrolls. The root then does NOT scroll itself.
   */
  appSurface: { type: Boolean, default: false },
})

const emit = defineEmits(['update:show', 'after-leave'])

// Reactive style — updated manually so we can react to DOM geometry changes
// (scroll, resize) that Vue's computed cannot track.
const menuStyle = ref({})

/** The rendered menu element; measured to find its containing block. */
const menuEl = ref(null)

/** Recalculate position from current anchor geometry. */
function updatePosition() {
  if (!props.targetElement) { menuStyle.value = {}; return }
  const rect = props.targetElement.getBoundingClientRect()
  // Measure the menu's ACTUAL containing block before computing offsets.
  //
  // `position: fixed` does not mean "viewport-anchored": an ancestor with a
  // transform / `will-change: transform` (`.chat-message` in app mode) becomes
  // the containing block, and CSS `zoom` scales the offsets. Feeding viewport
  // coordinates straight into `top`/`left` then lands the menu tens of pixels
  // off — measured 60px below the anchor inside a will-change ancestor.
  //
  // `measureFixedContainingBlock` probes with a CLONE of this element (see
  // fixedPosition.ts), so the live menu is never moved while it runs — an
  // earlier version wrote the probe offsets onto the real element, and the
  // browser painted the menu in its containing block's corner between the
  // write and the restore (Playwright: "element is not stable", then
  // "outside of the viewport" once it settled there).
  //
  // It needs the menu laid out, so read it only when the element is actually
  // rendered; otherwise fall back to the zoom-only conversion (still correct
  // for the common untransformed case).
  //
  // `getClientRects().length` — not `offsetParent` — is the check that works
  // here: a fixed element's `offsetParent` is null even when it IS laid out.
  const el = menuEl.value
  // `undefined` when the menu is not laid out yet — `computeMenuStyle` then uses
  // the zoom-only conversion, which is correct whenever no ancestor establishes
  // a containing block. Measuring a 0x0 box would instead report a bogus block
  // and place the menu off-screen (first-open symptom).
  const containingBlock = (el && el.getClientRects().length > 0
    ? measureFixedContainingBlock(el)
    : null) ?? undefined
  const measured = computeMenuStyle(rect, {
    maxWidth: props.maxWidth,
    maxHeight: props.maxHeight,
    edgeMargin: props.edgeMargin,
    menuItemsCount: props.menuItemsCount,
    anchor: props.anchor,
    scrollable: !props.appSurface,
    containingBlock,
  })
  // Sanity gate: a measured containing block must place the menu INSIDE the
  // viewport. If it does not, the measurement is wrong (a transformed ancestor,
  // a mid-Transition box, a 0x0 clone) and the zoom-only conversion is the
  // better answer — it is exact whenever nothing transforms, which covers the
  // overwhelming majority of menus. Without this gate a bad measurement put the
  // `#` completion menu 600px BELOW the viewport, where no row could be clicked.
  if (containingBlock && !menuFitsViewport(measured)) {
    menuStyle.value = computeMenuStyle(rect, {
      maxWidth: props.maxWidth,
      maxHeight: props.maxHeight,
      edgeMargin: props.edgeMargin,
      menuItemsCount: props.menuItemsCount,
      anchor: props.anchor,
      scrollable: !props.appSurface,
    })
    return
  }
  menuStyle.value = measured
}

/** Does the computed style keep the menu on screen? */
function menuFitsViewport(style) {
  const vh = window.innerHeight
  const vw = window.innerWidth
  const num = (v) => (v == null ? NaN : parseFloat(v))
  if (style.top != null) {
    const t = num(style.top)
    if (!Number.isFinite(t) || t > vh) return false
  }
  if (style.bottom != null) {
    const b = num(style.bottom)
    // A negative `bottom` means the box sits below the containing block's edge.
    if (!Number.isFinite(b) || b < 0) return false
  }
  if (style.left != null) {
    const l = num(style.left)
    if (!Number.isFinite(l) || l > vw) return false
  }
  if (style.right != null) {
    const r = num(style.right)
    if (!Number.isFinite(r) || r < 0) return false
  }
  return true
}

// Close on outside click
function handleClickOutside(e) {
  if (!props.targetElement) return
  if (props.targetElement.contains(e.target)) return
  if (e.target.closest('.popup-menu')) return
  emit('update:show', false)
}

// Recalculate on scroll/resize while open
function onLayoutChange() {
  if (props.show) updatePosition()
}

/**
 * Close when the anchor stops being laid out at all.
 *
 * The popup is teleported to <body>, so a parent that hides its panel with
 * `v-show` (display:none) does NOT hide the menu — it stays floating over
 * whatever replaced it. Reproduced: opening the notifications filter dropdown
 * and switching tabs left the menu on screen over the new tab.
 *
 * The signal must distinguish "hidden" from "merely scrolled out of view":
 *   - display:none          -> boundingClientRect is 0x0
 *   - scrolled out of view  -> isIntersecting false, but REAL dimensions
 * So we key off the zero size, not `isIntersecting`. Closing on a plain
 * scroll-out would be a behaviour change for every consumer (e.g. an
 * autocomplete whose input scrolls out of a short container), which is not
 * what this guards.
 */
let visibilityObserver = null

function observeAnchorVisibility() {
  if (typeof IntersectionObserver === 'undefined') return // jsdom / old WebView
  const el = props.targetElement
  if (!el) return
  visibilityObserver = new IntersectionObserver((entries) => {
    for (const entry of entries) {
      const r = entry.boundingClientRect
      if (r.width === 0 && r.height === 0) {
        emit('update:show', false)
        return
      }
    }
  })
  visibilityObserver.observe(el)
}

function unobserveAnchorVisibility() {
  if (visibilityObserver) {
    visibilityObserver.disconnect()
    visibilityObserver = null
  }
}

function bindOpenListeners() {
  // Listen for layout changes that could move the anchor
  window.addEventListener('scroll', onLayoutChange, true) // capture to catch all scrolls
  window.addEventListener('resize', onLayoutChange)
  // On mobile, soft keyboard show/hide triggers visualViewport resize but
  // NOT window.resize (iOS, PWA standalone, or Android non-adjustResize).
  // Listen to both so the popup repositions after keyboard state changes.
  if (window.visualViewport) {
    window.visualViewport.addEventListener('resize', onLayoutChange)
    window.visualViewport.addEventListener('scroll', onLayoutChange)
  }
  observeAnchorVisibility()
  // Use setTimeout to avoid the opening click being treated as outside click
  setTimeout(() => {
    if (props.show) {
      document.addEventListener('click', handleClickOutside)
    }
  }, 0)
}

function unbindOpenListeners() {
  window.removeEventListener('scroll', onLayoutChange, true)
  window.removeEventListener('resize', onLayoutChange)
  if (window.visualViewport) {
    window.visualViewport.removeEventListener('resize', onLayoutChange)
    window.visualViewport.removeEventListener('scroll', onLayoutChange)
  }
  unobserveAnchorVisibility()
  document.removeEventListener('click', handleClickOutside)
}

/** Position the menu for the current anchor geometry, one frame later. */
function schedulePosition() {
  // Compute position — defer one frame so that a soft keyboard dismissal
  // triggered by the same tap can begin before we read getBoundingClientRect().
  // The menu is inside a Transition so it won't paint until the next tick anyway.
  requestAnimationFrame(() => {
    if (!props.show) return // may have been closed already
    updatePosition()
  })
}

watch(() => props.show, (val) => {
  if (val) {
    schedulePosition()
    bindOpenListeners()
  } else {
    unbindOpenListeners()
  }
})

// A consumer may swap the anchor element while the menu is open — the Android
// input recovery rebuilds the chat textarea (:key) and rebinds the ref, and the
// terminal swaps between two buttons for PC/mobile layouts. Position is only
// recomputed on `show` changes, scroll and resize, so without this the menu
// stays anchored to the removed element's rect. Defer one frame for the same
// reason schedulePosition does: let layout settle before reading geometry.
// The visibility observer must follow the new element too, or it keeps watching
// a detached node and never fires.
watch(() => props.targetElement, () => {
  if (props.show) {
    schedulePosition()
    unobserveAnchorVisibility()
    observeAnchorVisibility()
  }
})

// A parent may mount this component with `show` already true (e.g. a menu whose
// own `v-if` is driven by the same condition that opens it). The `show` watcher
// above only fires on a *change*, so without this the menu would render
// unpositioned — a static block in the teleport target instead of a fixed
// popup. Position and bind on mount too.
onMounted(() => {
  if (props.show) {
    schedulePosition()
    bindOpenListeners()
  }
})

// Back (Android hardware button, predictive back, web edge swipe) closes the
// menu. Without this the press fell through to whatever was behind it — for the
// usage-meter popover that meant the press was consumed by a page-level handler
// and the popover stayed on screen, i.e. the only way out was tapping elsewhere.
//
// Same tier as the dialogs (`PRIORITY_OVERLAY + 1`) rather than a tier above:
// equal priorities are resolved by registration order, and every one of these
// surfaces registers when it OPENS — so the tie-break reproduces open order,
// which is also paint order (`--z-popover` 9999 > `--z-modal` 3000). Hard-coding
// a higher number would instead let a menu that was already open outrank a
// dialog raised on top of it.
const BACK_PRIORITY = PRIORITY_OVERLAY + 1
let unregisterBack = null

function registerBackHandlerForMenu() {
  if (unregisterBack) return
  unregisterBack = registerBackHandler({
    id: `popup-menu-${instanceSeq}`,
    canGoBack: () => props.show,
    goBack: () => emit('update:show', false),
    priority: BACK_PRIORITY,
  })
}

function unregisterBackHandlerForMenu() {
  if (!unregisterBack) return
  unregisterBack()
  unregisterBack = null
}

watch(() => props.show, (val) => {
  if (val) registerBackHandlerForMenu()
  else unregisterBackHandlerForMenu()
}, { immediate: true })

// Cleanup on unmount
onBeforeUnmount(() => {
  unregisterBackHandlerForMenu()
  unbindOpenListeners()
})
</script>

<style scoped>
.popup-menu {
  background: var(--bg-secondary, #fff);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: var(--radius-sm);
  box-shadow: 0 -4px 12px rgba(0, 0, 0, 0.12);
  z-index: var(--z-popover);
  padding: 0;
}

/* Fade animation for menu appearance */
.menu-fade-enter-active,
.menu-fade-leave-active {
  transition: opacity var(--duration-base) ease, transform var(--duration-base) ease;
}

.menu-fade-enter-from,
.menu-fade-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

/* ── App-header dropdown surface (matches .app-menu) ──
   When a popup is styled like the app-header dropdown (project switch, theme
   pickers), mirror its container look: primary background, downward shadow and
   subtle vertical padding. The root itself must NOT scroll (inline overflowY
   is omitted via computeMenuStyle scrollable:false) — the slot content is
   expected to be an .app-menu-column (header + .app-menu-scroll + footer)
   whose scrollable middle region absorbs the height left over by the pinned
   header/footer. max-height comes from the inline style computed by
   computeMenuStyle. */
.popup-menu--app {
  background: var(--bg-primary);
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.1);
  padding: 3px 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}
</style>
