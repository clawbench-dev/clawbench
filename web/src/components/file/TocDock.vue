<template>
  <div class="toc-dock" :class="`toc-dock--${side}`" :style="dockStyle">
    <!-- Drag divider to resize (mirrors SplitDivider interaction/style).
         On the right-side dock it sits on the dock's LEFT edge; on the
         left-side dock it sits on the dock's RIGHT edge. -->
    <div
      ref="dividerRef"
      class="toc-dock-divider"
      role="separator"
      aria-orientation="vertical"
      @pointerdown="startDrag"
      :title="t('toc.dragResize')"
    >
      <div class="toc-dock-divider__line" />
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
let dragging = false
let startClientX = 0
let startWidth = 0

function startDrag(e) {
  if (e.button !== 0) return
  dragging = true
  startClientX = e.clientX
  startWidth = tocDockWidth.value
  dividerRef.value?.setPointerCapture?.(e.pointerId)
  document.body.classList.add('toc-dock-resizing')
}

function onDragMove(e) {
  if (!dragging) return
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
  if (!dragging) return
  dragging = false
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
  min-width: 200px;
  max-width: 400px;
  flex-shrink: 0;
  background: var(--bg-secondary);
  /* No border-left: the drag divider's gutter-line (centered on this edge)
     already provides the separator — combining both would render a thick
     double line between the content area and the dock.
     No border-top: the FileHeader's bottom border already separates it. */
  overflow: hidden;
}

/* Divider (mirrors SplitDivider): a single 1px line by default; on hover/drag
   it expands (via negative margins so layout does NOT shift) into a grab-able
   gap with an accent highlight. Sits on the dock's LEFT edge when docked
   right; moves to the RIGHT edge when docked left. */
.toc-dock-divider {
  position: absolute;
  left: -3px;
  top: 0;
  bottom: 0;
  width: 6px;
  cursor: col-resize;
  touch-action: none;
  -webkit-tap-highlight-color: transparent;
  z-index: 5;
  transition: width var(--duration-base) ease, margin var(--duration-base) ease, background var(--duration-base) ease;
}
.toc-dock--left .toc-dock-divider {
  left: auto;
  right: -3px;
}
/* invisible wider hit area so hover/touch can catch the thin line */
.toc-dock-divider::before {
  content: '';
  position: absolute;
  top: 0;
  bottom: 0;
  left: -4px;
  right: -4px;
}
.toc-dock-divider:active {
  width: 12px;
  margin-left: -3px;
  background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
}
@media (hover: hover) {
  .toc-dock-divider:hover {
    width: 12px;
    margin-left: -3px;
    background: color-mix(in srgb, var(--accent-color, #0066cc) 12%, transparent);
  }
}
/* Left-docked: the divider hangs off the RIGHT edge, so the hover/drag
   expansion must shift RIGHT (margin-left: 3px) to stay centered on it. */
.toc-dock--left .toc-dock-divider:active,
.toc-dock--left .toc-dock-divider:hover {
  margin-left: 3px;
}
.toc-dock-divider__line {
  position: absolute;
  left: 50%;
  top: 0;
  bottom: 0;
  width: 1px;
  transform: translateX(-50%);
  background: var(--border-color, rgba(0, 0, 0, 0.12));
  transition: background var(--duration-base) ease;
}
.toc-dock-divider:active .toc-dock-divider__line,
.toc-dock-divider:hover .toc-dock-divider__line {
  background: var(--accent-color, #0066cc);
}

.toc-dock-header {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  padding: var(--space-2) var(--space-3);
  border-bottom: 1px solid var(--border-color);
  flex-shrink: 0;
  min-height: 28px;
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
