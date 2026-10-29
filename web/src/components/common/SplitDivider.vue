<template>
  <div
    ref="dividerRef"
    class="split-view__divider resize-divider"
    :class="[`split-view__divider--${orientation}`, { 'resize-divider--expanded': dragging, 'split-view__divider--revealed': revealed, 'split-view__divider--lingering': lingering }]"
    role="separator"
    :aria-orientation="ariaOrientation"
    :aria-valuenow="ariaValueNow"
    :aria-valuemin="ariaValueMin"
    :aria-valuemax="ariaValueMax"
    :title="resolvedTitle"
    @pointerdown="onDividerPointerDown"
    @pointerenter="onDividerPointerEnter"
    @pointerleave="onDividerPointerLeave"
  >
    <div class="resize-divider__line" />
    <!-- Collapse handles: one on each side of the line, so either pane can be
         hidden from the same boundary. They live INSIDE the divider so hovering
         the line reveals them and so that moving the pointer onto a handle
         keeps the divider `:hover` (a hovered descendant keeps its ancestors
         hovered) — an out-of-tree overlay would flicker the moment the pointer
         left the 1px line.

         Inline SVG rather than lucide components: this file sits under
         SplitView, which several tests import with a narrow `lucide-vue-next`
         mock that only lists a handful of icons. -->
    <button
      v-if="collapseLeft"
      type="button"
      class="split-view__collapse-btn split-view__collapse-btn--first"
      data-collapse="left"
      :title="resolvedCollapseLeftTitle"
      :aria-label="resolvedCollapseLeftTitle"
      @pointerdown.stop
      @click.stop="emit('collapse', 'left')"
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <rect width="18" height="18" x="3" y="3" rx="2" />
        <path :d="firstHandleGlyph.divider" />
        <path :d="firstHandleGlyph.arrow" />
      </svg>
    </button>
    <button
      v-if="collapseRight"
      type="button"
      class="split-view__collapse-btn split-view__collapse-btn--second"
      data-collapse="right"
      :title="resolvedCollapseRightTitle"
      :aria-label="resolvedCollapseRightTitle"
      @pointerdown.stop
      @click.stop="emit('collapse', 'right')"
    >
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <rect width="18" height="18" x="3" y="3" rx="2" />
        <path :d="secondHandleGlyph.divider" />
        <path :d="secondHandleGlyph.arrow" />
      </svg>
    </button>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()

const props = withDefaults(defineProps<{
  /** 'horizontal' = left/right panes (vertical divider line, drag on X).
   *  'vertical'   = top/bottom panes (horizontal divider line, drag on Y). */
  orientation?: 'horizontal' | 'vertical'
  title?: string
  ariaValueNow?: number
  ariaValueMin?: number
  ariaValueMax?: number
  /** Show a collapse handle on the FIRST-pane side of the line. Omitted ⇒ no
   *  handle, which keeps existing consumers (session sidebar, TOC dock, file
   *  manager) unchanged. */
  collapseLeft?: boolean
  /** Show a collapse handle on the SECOND-pane side of the line. */
  collapseRight?: boolean
  /** Tooltips for the two handles; each falls back to a generic i18n label. */
  collapseLeftTitle?: string
  collapseRightTitle?: string
}>(), {
  orientation: 'horizontal',
})

/**
 * The aria title. Resolved in a computed rather than a prop default so it
 * follows the live locale: `withDefaults` is evaluated once at setup, before
 * any reactive locale change, and would freeze the string in the boot language.
 */
const resolvedTitle = computed(() => props.title ?? t('common.dragToResize'))
const resolvedCollapseLeftTitle = computed(() => props.collapseLeftTitle ?? t('common.close'))
const resolvedCollapseRightTitle = computed(() => props.collapseRightTitle ?? t('common.close'))

const isVertical = computed(() => props.orientation === 'vertical')

/**
 * The glyphs of the two handles, as `[divider path, arrow path]` pairs.
 *
 * These are lucide's `panel-{left,right,bottom,top}-close` icons inlined: a
 * rounded rectangle with a divider line and a chevron inside the pane being
 * hidden. A bare chevron was read as a drag handle (the same glyph a
 * scrollbar/slider wears), so the frame + divider line is what says "pane".
 *
 * Inline SVG rather than the lucide components because this file sits under
 * SplitView, which several tests import with a narrow `lucide-vue-next` mock
 * that only lists a handful of icons — a new import would come back
 * `undefined` and crash the render.
 */
const firstHandleGlyph = computed(() => {
  if (isVertical.value) {
    // Top pane is hidden: divider under the header, chevron pointing up.
    return { divider: 'M3 9h18', arrow: 'm9 16 3-3 3 3' }
  }
  // Left pane is hidden: divider on the left, chevron pointing left.
  return { divider: 'M9 3v18', arrow: 'm16 15-3-3 3-3' }
})

const secondHandleGlyph = computed(() => {
  if (isVertical.value) {
    // Bottom pane is hidden: divider above the footer, chevron pointing down.
    return { divider: 'M3 15h18', arrow: 'm15 8-3 3-3-3' }
  }
  // Right pane is hidden: divider on the right, chevron pointing right.
  return { divider: 'M15 3v18', arrow: 'm8 9 3 3-3 3' }
})

/** A vertical divider separates left/right panes; a horizontal one separates
 *  top/bottom. ARIA describes the separator line itself. */
const ariaOrientation = computed(() => (isVertical.value ? 'horizontal' : 'vertical'))

const emit = defineEmits<{
  (e: 'dragstart'): void
  (e: 'dragmove', clientPos: number): void
  (e: 'dragend'): void
  (e: 'collapse', side: 'left' | 'right'): void
}>()

const dividerRef = ref<HTMLDivElement | null>(null)
/**
 * Drives the expanded highlight while dragging. `:active` alone is not enough
 * on touch: once `setPointerCapture` takes over the pointer, the browser drops
 * `:active`, so a fast swipe showed no highlight at all (only a held press did).
 * This mirrors the pointer lifecycle instead of relying on the pseudo-class.
 */
const dragging = ref(false)
let dragActive = false

/**
 * Whether the collapse handles are showing on a TOUCH device.
 *
 * Mouse/pen users get them from `:hover`; touch has no hover at all, so a
 * tap-the-line-to-reveal gesture is the only way to reach them without parking
 * two floating buttons on top of the panes forever. The handles then retract on
 * their own (`REVEAL_HOLD_MS`) or as soon as the user taps anywhere else.
 */
const revealed = ref(false)
let revealTimer: ReturnType<typeof setTimeout> | null = null

/**
 * Whether the pointer has left the divider and the handles are in their
 * leaving sequence.
 *
 * Leaving is two steps, not one: the handles fade to `--opacity-soft` (0.7)
 * and HOLD there for `HANDLE_LINGER_MS`, then fade the rest of the way out.
 * The dimmed hold is what makes the button reachable from a nearby position —
 * a single 150ms 1 → 0 fade meant the target was gone before the user could
 * move onto it. Being visible, it must also stay clickable, so
 * `pointer-events` follows this state.
 */
const lingering = ref(false)
let lingerTimer: ReturnType<typeof setTimeout> | null = null
let dwellTimer: ReturnType<typeof setTimeout> | null = null
/** Where the current press started, to tell a tap from a drag. */
let pressStart: { x: number; y: number } | null = null
let pressMoved = false

/** How long the handles stay up after a tap, in ms. */
const REVEAL_HOLD_MS = 3000

/**
 * Grace period (ms) the handles stay up after the pointer leaves the divider.
 *
 * Leaving the 1px line used to fade them out over 150ms, so a user who moved
 * away and came back had to re-find the line — the target had already
 * vanished. Holding them for a moment keeps the buttons clickable from a
 * nearby position. Short enough that merely crossing the divider on the way
 * somewhere else does not flash two discs at the user.
 */
const HANDLE_LINGER_MS = 1000

/**
 * Dwell (ms) that counts as "the user was looking at the divider" rather than
 * "the pointer passed over it". Only a dwell starts the linger: the hover fade
 * itself takes `--duration-base` (150ms), so anything shorter never showed the
 * handles clearly and there is nothing worth keeping on screen.
 */
const HOVER_DWELL_MS = 200
/**
 * Movement (CSS px) beyond which a press is a DRAG, not a tap. Generous on
 * purpose: the divider is a 1px line with a fat invisible hit band, and fingers
 * wobble. Revealing the handles when someone meant to resize (or vice versa) is
 * the failure this guards against.
 */
const TAP_SLOP_PX = 8

function clearRevealTimer() {
  if (revealTimer !== null) {
    clearTimeout(revealTimer)
    revealTimer = null
  }
}

function clearLingerTimers() {
  if (lingerTimer !== null) {
    clearTimeout(lingerTimer)
    lingerTimer = null
  }
  if (dwellTimer !== null) {
    clearTimeout(dwellTimer)
    dwellTimer = null
  }
}

function hideHandles() {
  clearRevealTimer()
  clearLingerTimers()
  revealed.value = false
  lingering.value = false
}

function scheduleHide() {
  clearRevealTimer()
  revealTimer = setTimeout(() => {
    revealed.value = false
    revealTimer = null
  }, REVEAL_HOLD_MS)
}

/**
 * Pointer entered the divider. Starting the dwell timer here (rather than in
 * CSS) is what lets `onDividerPointerLeave` tell "hovered long enough to aim"
 * from "crossed on the way to somewhere else".
 */
function onDividerPointerEnter(e: PointerEvent) {
  if (e.pointerType === 'touch') return
  clearLingerTimers()
  // The pointer is back on the line: the leave sequence is cancelled and
  // `:hover` takes the handles straight back to full strength.
  lingering.value = false
  dwellTimer = setTimeout(() => {
    dwellTimer = null
  }, HOVER_DWELL_MS)
}

/**
 * Pointer left the divider.
 *
 * `:hover` drops the handles the instant the pointer leaves the line, so the
 * leave sequence has to be re-anchored to a class we control: `--lingering`
 * takes them to `--opacity-soft` (0.7) and HOLDS there for
 * `HANDLE_LINGER_MS`; clearing the class then fades the rest of the way to 0.
 * Without it, moving off the 1px line and back meant the buttons were already
 * gone.
 *
 * Skipped when the pointer never dwelt (see `HOVER_DWELL_MS`) — merely
 * crossing the divider on the way somewhere else must not flash two glyphs —
 * and while a drag is in flight (a drag ends with `pointerup`, not with the
 * pointer leaving).
 */
function onDividerPointerLeave(e: PointerEvent) {
  if (e.pointerType === 'touch') return
  const dwelt = dwellTimer === null
  clearLingerTimers()
  if (!dwelt || dragActive) return
  lingering.value = true
  lingerTimer = setTimeout(() => {
    lingering.value = false
    lingerTimer = null
  }, HANDLE_LINGER_MS)
}

/**
 * Dismiss on any press that is not on the divider (or on one of its handles).
 * Capture phase, so a press on a handle counts as "inside" before the handle's
 * own `.stop` runs — otherwise tapping a handle would look like an outside tap
 * and tear the handles down under the finger.
 */
function onDocumentPointerDown(e: PointerEvent) {
  const root = dividerRef.value
  if (root && e.target instanceof Node && root.contains(e.target)) {
    // Still inside: keep them up while the user aims.
    scheduleHide()
    return
  }
  hideHandles()
}

function onDividerPointerDown(e: PointerEvent) {
  if (e.button !== 0) return
  pressStart = { x: e.clientX, y: e.clientY }
  pressMoved = false
  dragActive = true
  dragging.value = true
  dividerRef.value?.setPointerCapture?.(e.pointerId)
  document.body.classList.add('split-view-dragging')
  document.body.classList.add(isVertical.value ? 'split-view-dragging--vertical' : 'split-view-dragging--horizontal')
  emit('dragstart')
}

function onPointerMove(e: PointerEvent) {
  if (!dragActive) return
  if (pressStart && !pressMoved) {
    const dx = e.clientX - pressStart.x
    const dy = e.clientY - pressStart.y
    if (Math.hypot(dx, dy) > TAP_SLOP_PX) {
      pressMoved = true
      // A real drag is not a reveal: drop the handles so they cannot be
      // clicked by accident while the pointer is still captured.
      hideHandles()
    }
  }
  // Vertical split reads the pointer's Y; horizontal reads X.
  emit('dragmove', isVertical.value ? e.clientY : e.clientX)
}

function onPointerUp(e: PointerEvent) {
  if (!dragActive) return
  const wasTap = pressStart !== null && !pressMoved
  dragActive = false
  dragging.value = false
  pressStart = null
  dividerRef.value?.releasePointerCapture?.(e.pointerId)
  document.body.classList.remove('split-view-dragging')
  document.body.classList.remove('split-view-dragging--vertical', 'split-view-dragging--horizontal')
  emit('dragend')
  // Mouse and pen have hover, so a click must not toggle anything: the handles
  // are already up while the pointer is on the line.
  if (wasTap && e.pointerType !== 'mouse') {
    revealed.value = true
    scheduleHide()
  }
}

// The outside-press listener only exists while the handles are up; keeping it
// permanently attached would run on every press in the app.
watch(revealed, (on) => {
  if (on) window.addEventListener('pointerdown', onDocumentPointerDown, true)
  else window.removeEventListener('pointerdown', onDocumentPointerDown, true)
})

onMounted(() => {
  window.addEventListener('pointermove', onPointerMove)
  window.addEventListener('pointerup', onPointerUp)
  window.addEventListener('pointercancel', onPointerUp)
})

onBeforeUnmount(() => {
  window.removeEventListener('pointermove', onPointerMove)
  window.removeEventListener('pointerup', onPointerUp)
  window.removeEventListener('pointercancel', onPointerUp)
  window.removeEventListener('pointerdown', onDocumentPointerDown, true)
  clearRevealTimer()
  clearLingerTimers()
  document.body.classList.remove('split-view-dragging')
  document.body.classList.remove('split-view-dragging--vertical', 'split-view-dragging--horizontal')
})
</script>

<style scoped>
/* Divider: a single 1px line by default — no visible gap. On hover/drag the
   line ITSELF turns accent and thickens to 2px: the host grows 1px → 2px (via
   negative margins so layout does NOT shift) and the shared line rules fill it.
   There is deliberately NO tinted band — a band straddles the divider's edge,
   and against a pane it covered the adjacent scrollbar. Only a thin line is
   highlighted now.

   The line itself (resting thickness, the accent colour and its hover/drag
   triggers) lives in the shared `assets/resize-divider.css` — the same file
   TocDock.vue consumes. Only the host geometry stays here, because it differs
   per consumer. */
.split-view__divider {
  position: relative;
  flex: 0 0 auto;
  margin: 0;
  touch-action: none;
  -webkit-tap-highlight-color: transparent;
  z-index: 2;
  transition: width var(--duration-base) ease, height var(--duration-base) ease, margin var(--duration-base) ease;
}

/* ── Horizontal split (left | right): vertical 1px line, drag on X ── */
.split-view__divider--horizontal {
  width: var(--split-gutter, 1px);
  cursor: col-resize;
}
/* Invisible hit area so hover/touch can catch the 1px line. ±4px (9px total):
   enough for a mouse, narrow enough not to swallow the scrollbar that sits
   flush against the pane edge. */
.split-view__divider--horizontal::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: calc(-4 * var(--dp));
  right: calc(-4 * var(--dp));
}
/* 2px highlight. `:active` covers mouse press; `--expanded` covers the whole
   pointer session, which is what touch needs (see the `dragging` ref).
   `margin: 0 -0.5px` widens the 1px host symmetrically about its edge:
   (2 - 1) / 2 = 0.5, so the outer footprint stays 1px and neither pane shifts.
   This block must stay in lockstep with the `(hover: hover)` block below —
   both are host geometry, the line's own rules are shared. */
.split-view__divider--horizontal:active,
.split-view__divider--horizontal.resize-divider--expanded {
  width: calc(2 * var(--dp));
  margin: 0 -0.5px;
}
@media (hover: hover) {
  .split-view__divider--horizontal:hover {
    width: calc(2 * var(--dp));
    margin: 0 -0.5px;
  }
}

/* ── Vertical split (top / bottom): horizontal 1px line, drag on Y ── */
.split-view__divider--vertical {
  height: var(--split-gutter, 1px);
  cursor: row-resize;
}
/* Invisible hit area — ±4px, same rationale as the horizontal band above. */
.split-view__divider--vertical::before {
  content: '';
  position: absolute;
  left: 0;
  right: 0;
  top: calc(-4 * var(--dp));
  bottom: calc(-4 * var(--dp));
}
/* Touch: a 1px line is an unhittable target (and the negative-margin hover
   growth is hover-only). Give touch pointers a fatter invisible grab band via
   the pseudo-element, without changing the visual 1px line. */
@media (pointer: coarse) {
  .split-view__divider--vertical::before {
    top: calc(-12 * var(--dp));
    bottom: calc(-12 * var(--dp));
  }
  .split-view__divider--horizontal::before {
    left: calc(-12 * var(--dp));
    right: calc(-12 * var(--dp));
  }
  /* A slightly thicker resting line reads as draggable on touch. */
  .split-view__divider--vertical {
    height: calc(3 * var(--dp));
  }
  .split-view__divider--horizontal {
    width: calc(3 * var(--dp));
  }
}
.split-view__divider--vertical:active,
.split-view__divider--vertical.resize-divider--expanded {
  height: calc(2 * var(--dp));
  margin: -0.5px 0;
}
@media (hover: hover) {
  .split-view__divider--vertical:hover {
    height: calc(2 * var(--dp));
    margin: -0.5px 0;
  }
}

/* ── Collapse handles ──
   One button on EACH side of the line, each hiding the pane it points at.
   They are children of the divider but positioned just outside its 1px box, so
   they read as "belonging to" the pane they will hide rather than to the line.

   THE GLYPH IS THE BUTTON — no disc, no ring, no fill, no shadow box. An earlier
   version wrapped the icon in a 28px circle with a `color-mix` fill and a ring;
   the frame read as a floating chip of UI rather than as the pane control, and
   it competed with the divider line it sits next to. The button is now exactly
   the icon's box.

   At rest they are INVISIBLE on touch and SEMI-TRANSPARENT on hover-capable
   pointers (see the `(hover: hover)` block below). Invisible still has to mean
   inert — an invisible click target floating over the pane edge would swallow
   clicks meant for the content beside it — so `pointer-events` tracks the same
   conditions as `opacity` (including `:focus-within`, without which a keyboard
   user could focus a button they cannot see). Once the glyph is actually
   visible, the reverse holds: a visible control that cannot be clicked is a
   lie, so the resting state is clickable too.

   Being DOM children is what makes the reveal stable: `:hover` propagates to
   ancestors of the hovered element, so moving the pointer off the 1px line and
   onto a handle keeps the divider hovered. A body-level overlay would flicker
   the instant the pointer left the line.

   CONTRAST — with no fill there is nothing between the stroke and whatever the
   pane happens to show underneath (text, images, a scrollbar). The glyph is
   drawn in `--text-primary` (already the theme's high-contrast ink on a
   `--bg-secondary` pane), and a two-pass `drop-shadow` halo in the PANE colour
   separates the stroke from busy content. The halo must use the pane token, not
   a fixed black/white: 36 themes split into light and dark, and a one-sided
   shadow disappears on the other half.

   SIZING — scales, but with a CAP. The icon follows `--ui-scale` like the rest
   of the UI, and `min()` stops it at 40px. Sizing is chosen so the DRAWN frame
   (the icon's rect is 18 of the 24 viewBox units, i.e. 75% of the box) is at
   least twice the old 12dp chevron: 32dp × 0.75 = 24dp drawn, vs 12dp before.
   Sizing the box by the old glyph number would have doubled nothing. The cap is
   a literal px on purpose (like `--radius-full`) — it exists precisely to STOP
   following the scale. */
.split-view__collapse-btn {
  position: absolute;
  width: min(calc(32 * var(--dp)), 40px);
  height: min(calc(32 * var(--dp)), 40px);
  padding: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 0;
  border-radius: 0;
  background: none;
  box-shadow: none;
  color: var(--text-primary);
  filter: drop-shadow(0 0 2px var(--bg-secondary)) drop-shadow(0 0 1px var(--bg-secondary));
  cursor: pointer;
  opacity: 0;
  pointer-events: none;
  transition: opacity var(--duration-base) ease, color var(--duration-base);
  -webkit-tap-highlight-color: transparent;
}
/* The SVG fills the button box, so the button and the glyph are the same size —
   there is no padding to make one read as a surface under the other. */
.split-view__collapse-btn svg {
  width: 100%;
  height: 100%;
}
/* ── Reveal / leave sequence ──
   Hover in:     0 → 1
   Hover out:    1 → 0.7, HOLD for HANDLE_LINGER_MS, then 0.7 → 0

   The two-step leave is the point. A single 1 → 0 fade (150ms) removed the
   target before a user standing slightly away could move onto it; dimming
   first and holding means the button stays visible and clickable during the
   window where the user is actually reaching for it, and only then fades out
   for good. The hold is a state, not a delay on the transition, so the fade
   itself still uses `--duration-base`.

   `--lingering` therefore means "dimmed and holding", NOT "still full
   strength" — it is the middle rung, and `pointer-events` follows it because a
   visible control that ignores clicks is a lie. */
.split-view__divider:hover .split-view__collapse-btn,
.split-view__divider:focus-within .split-view__collapse-btn {
  opacity: 1;
  pointer-events: auto;
}
.split-view__divider--lingering .split-view__collapse-btn {
  opacity: var(--opacity-soft);
  pointer-events: auto;
}
@media (hover: hover) {
  .split-view__collapse-btn:hover {
    color: var(--accent-color);
  }
}
.split-view__collapse-btn:focus-visible {
  outline: 2px solid var(--accent-color);
  outline-offset: 1px;
}

/* ── Handle placement ──
   Each orientation needs its own pair of offsets: the panes sit on the X axis
   for a horizontal split and on the Y axis for a vertical one. The gap keeps
   the handles clear of the pane's scrollbar (which is flush against the edge),
   so a hover does not read as "the scrollbar grew a button" — and, at the size
   the glyphs are now, also keeps them from crowding the pane's first line of
   content. It was 4dp (capped 6px), which put the 40px glyph right up against
   the pane edge; 8dp (capped 12px) gives the boundary visible breathing room.
   The gap takes a cap as well so the pair does not drift away from the line on
   a large UI scale. */
.split-view__divider--horizontal .split-view__collapse-btn--first {
  top: 50%;
  left: 0;
  transform: translate(calc(-100% - min(var(--space-4), 12px)), -50%);
}
.split-view__divider--horizontal .split-view__collapse-btn--second {
  top: 50%;
  left: 100%;
  transform: translate(min(var(--space-4), 12px), -50%);
}
.split-view__divider--vertical .split-view__collapse-btn--first {
  left: 50%;
  top: 0;
  transform: translate(-50%, calc(-100% - min(var(--space-4), 12px)));
}
.split-view__divider--vertical .split-view__collapse-btn--second {
  left: 50%;
  top: 100%;
  transform: translate(-50%, min(var(--space-4), 12px));
}

/* Touch has no hover, so the handles are revealed by TAPPING the line instead
   of being parked on top of the panes permanently: `--revealed` is set by the
   tap handler and cleared after a hold or on any press elsewhere. The target is
   bigger than the mouse one (a finger needs it), capped for the same reason as
   the base size. The glyph scales with the button (svg is 100%), so only the
   box is overridden here. */
@media (pointer: coarse) {
  .split-view__divider--revealed .split-view__collapse-btn {
    opacity: 1;
    pointer-events: auto;
    width: min(calc(40 * var(--dp)), 50px);
    height: min(calc(40 * var(--dp)), 50px);
  }
}

:global(body.split-view-dragging) {
  user-select: none;
}
:global(body.split-view-dragging--horizontal) {
  cursor: col-resize;
}
:global(body.split-view-dragging--vertical) {
  cursor: row-resize;
}
</style>

<style>
/* Shared with TocDock.vue — NOT scoped, so the class names stay literal
   instead of getting a per-component data-v attribute. Both dividers must
   receive the identical line rules. */
@import '@/assets/resize-divider.css';
</style>
