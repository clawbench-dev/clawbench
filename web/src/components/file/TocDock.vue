<template>
  <div class="toc-dock" :class="`toc-dock--${side}`" :style="dockStyle">
    <!-- Drag divider to resize (mirrors SplitDivider interaction/style).
         On the right-side dock it sits on the dock's LEFT edge; on the
         left-side dock it sits on the dock's RIGHT edge. -->
    <div
      ref="dividerRef"
      class="toc-dock-divider resize-divider"
      :class="{ 'resize-divider--expanded': dragging }"
      role="separator"
      aria-orientation="vertical"
      @pointerdown="startDrag"
      :title="t('toc.dragResize')"
    >
      <div class="resize-divider__line" />
    </div>

    <div class="toc-dock-header">
      <List :size="12" class="toc-dock-header-icon" />
      <span class="toc-dock-header-title">{{ t('toc.title') }}</span>
      <button
        class="toc-dock-side-toggle"
        @click="toggleSide"
        :title="side === 'left' ? t('toc.dockRight') : t('toc.dockLeft')"
      >
        <PanelRight v-if="side === 'left'" :size="12" />
        <PanelLeft v-else :size="12" />
      </button>
      <button class="toc-dock-close" @click="emit('close')" :title="t('common.close')">
        <X :size="12" />
      </button>
    </div>

    <TocPanel
      open
      :file="file"
      :pdf-outline="pdfOutline"
      :code-view="codeView"
      @jump="(line, anchorId) => emit('jump', line, anchorId)"
      @jump-page="emit('jumpPage', $event)"
    />
  </div>
</template>

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { List, PanelLeft, PanelRight, X } from 'lucide-vue-next'
import TocPanel from '@/components/TocPanel.vue'
import { useTocDockPreference } from '@/composables/useTocDockPreference'

const props = defineProps({
  file: Object,
  pdfOutline: { type: Array, default: () => [] },
  /** Whether the underlying content is rendered by CodeMirror (drives scroll-follow). */
  codeView: { type: Boolean, default: false },
  /** Which edge of the content area the dock is attached to. */
  side: { type: String, default: 'right' },
})
const emit = defineEmits(['close', 'jump', 'jumpPage'])

const { t } = useI18n()
const { tocDockWidth, setWidth, toggleSide } = useTocDockPreference()

const dockStyle = computed(() => ({ width: `${tocDockWidth.value}px` }))

// ── Drag-to-resize (mirrors SplitDivider: pointer capture + body class) ──
const dividerRef = ref(null)
/**
 * Drives the expanded highlight for the whole drag. `:active` is not enough on
 * touch — `setPointerCapture` makes the browser drop it, so a fast swipe showed
 * no highlight (only a held press did). Mirrors the pointer lifecycle instead.
 */
const dragging = ref(false)
let startClientX = 0
let startWidth = 0

function startDrag(e) {
  if (e.button !== 0) return
  dragging.value = true
  startClientX = e.clientX
  startWidth = tocDockWidth.value
  dividerRef.value?.setPointerCapture?.(e.pointerId)
  document.body.classList.add('toc-dock-resizing')
}

function onDragMove(e) {
  if (!dragging.value) return
  const delta = e.clientX - startClientX
  if (props.side === 'left') {
    // Left-side dock: the divider is its RIGHT edge, which follows the
    // pointer. Dragging right widens the dock, dragging left narrows it —
    // so the delta is ADDED.
    setWidth(startWidth + delta)
  } else {
    // Right-side dock: the divider is its LEFT edge. Dragging left widens
    // the dock, dragging right narrows it — the delta is SUBTRACTED.
    setWidth(startWidth - delta)
  }
}

function endDrag(e) {
  if (!dragging.value) return
  dragging.value = false
  dividerRef.value?.releasePointerCapture?.(e.pointerId)
  document.body.classList.remove('toc-dock-resizing')
}

onMounted(() => {
  window.addEventListener('pointermove', onDragMove)
  window.addEventListener('pointerup', endDrag)
  window.addEventListener('pointercancel', endDrag)
})

onBeforeUnmount(() => {
  window.removeEventListener('pointermove', onDragMove)
  window.removeEventListener('pointerup', endDrag)
  window.removeEventListener('pointercancel', endDrag)
  document.body.classList.remove('toc-dock-resizing')
})
</script>

<style scoped>
.toc-dock {
  position: relative;
  display: flex;
  flex-direction: column;
  min-width: calc(200 * var(--dp));
  max-width: calc(400 * var(--dp));
  flex-shrink: 0;
  background: var(--bg-secondary);
  /* No border-left: the drag divider's line (on this edge) already provides the
     separator — combining both would render a thick double line between the
     content area and the dock.
     No border-top: the FileHeader's bottom border already separates it.

     `overflow: visible`, NOT `hidden`: the divider's `::before` grab band
     straddles this edge, and the 2px highlight grows 0.5px past it — a clipping
     dock would cut the content-side half away, leaving a 1px-wide grab zone and
     a lopsided highlight. The panel content cannot leak anyway — `.toc-list`
     scrolls internally and every label ellipsizes. */
  overflow: visible;
}

/* Divider (mirrors SplitDivider): a single 1px line at rest; on hover/drag the
   line itself turns accent and thickens to 2px — the host grows 1px → 2px via a
   negative margin so layout does NOT shift. No tinted band (see SplitDivider).
   Sits on the dock's LEFT edge when docked right; moves to the RIGHT edge when
   docked left.

   The line itself (resting thickness, the accent colour and its hover/drag
   triggers) lives in the shared `assets/resize-divider.css` — the same file
   SplitDivider.vue consumes. Only the host geometry stays here, because it
   differs per consumer.

   RESTING WIDTH IS 1px, not 6px: the line fills its host, so the host's width
   IS the rendered line. A 6px host painted a 6px-thick grey bar between the
   content and the dock — six times the 1px `--border-color` used by every other
   separator (FileHeader, dock header, SplitDivider), which is what read as
   "不对劲". The extra pixels bought nothing: the grab area is the `::before`
   band below, and `width` is the axis the shared file animates. */
.toc-dock-divider {
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 1px;
  cursor: col-resize;
  touch-action: none;
  -webkit-tap-highlight-color: transparent;
  z-index: 5;
  transition: width var(--duration-base) ease, margin var(--duration-base) ease;
}
.toc-dock--left .toc-dock-divider {
  left: auto;
  right: 0;
}
/* Invisible hit area so hover/touch can catch the thin line — ±4px (9px
   total), matching SplitDivider. Narrow enough not to swallow the 4px
   scrollbar sitting flush against the dock edge. */
.toc-dock-divider::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: calc(-4 * var(--dp));
  right: calc(-4 * var(--dp));
}
/* 2px highlight. `:active` covers mouse press; `--expanded` covers the whole
   pointer session, which is what touch needs (see the `dragging` ref).
   `margin-left: -0.5px` widens the 1px host symmetrically about the dock edge:
   (2 - 1) / 2 = 0.5, so the outer footprint stays 1px and the dock does not
   shift. This block must stay in lockstep with the `(hover: hover)` block
   below — both are host geometry, the line's own rules are shared. */
.toc-dock-divider:active,
.toc-dock-divider.resize-divider--expanded {
  width: calc(2 * var(--dp));
  margin-left: -0.5px;
}
@media (hover: hover) {
  .toc-dock-divider:hover {
    width: calc(2 * var(--dp));
    margin-left: -0.5px;
  }
}
/* Left-docked: the divider hangs off the RIGHT edge, so the growth must shift
   RIGHT to stay centered on it. */
.toc-dock--left .toc-dock-divider:active,
.toc-dock--left .toc-dock-divider.resize-divider--expanded,
.toc-dock--left .toc-dock-divider:hover {
  margin-left: 0;
  margin-right: -0.5px;
}

.toc-dock-header {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
  min-height: calc(28 * var(--dp));
}

.toc-dock-header-icon {
  flex-shrink: 0;
  color: var(--text-muted);
}

.toc-dock-header-title {
  flex: 1;
  font-size: var(--font-size-xs);
  font-weight: var(--font-weight-semibold);
  color: var(--text-primary);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.toc-dock-close {
  padding: var(--space-1);
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
.toc-dock-side-toggle {
  padding: var(--space-1);
  border: none;
  border-radius: var(--radius-xs);
  background: transparent;
  color: var(--text-secondary);
  cursor: pointer;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
@media (hover: hover) {
  .toc-dock-close:hover {
    background: color-mix(in srgb, var(--accent-color) 12%, transparent);
    color: var(--accent-color);
  }
  .toc-dock-side-toggle:hover {
    background: color-mix(in srgb, var(--accent-color) 12%, transparent);
    color: var(--accent-color);
  }
}
</style>

<style>
/* Prevent text selection while dragging the divider */
body.toc-dock-resizing {
  user-select: none;
  cursor: col-resize;
}
</style>

<style>
/* Shared with SplitDivider.vue — NOT scoped, so the class names stay literal
   instead of getting a per-component data-v attribute. Both dividers must
   receive the identical line rules. */
@import '@/assets/resize-divider.css';
</style>
