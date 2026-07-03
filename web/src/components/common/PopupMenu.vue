<template>
  <Teleport to="body">
    <Transition name="menu-fade">
      <div v-if="show" class="popup-menu" role="menu" :style="menuStyle" @click.stop="emit('update:show', false)" @keydown.escape="emit('update:show', false)">
        <slot />
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
import { ref, watch, onBeforeUnmount } from 'vue'
import { computeMenuStyle } from '@/utils/popupMenuPosition'

const props = defineProps({
  show: Boolean,
  targetElement: { type: Object }, // DOM element reference
  maxWidth: { type: Number, default: 220 },
  maxHeight: { type: Number, default: 320 },
  edgeMargin: { type: Number, default: 6 },
  menuItemsCount: { type: Number, default: 10 }, // for height estimation
  anchor: { type: String, default: 'auto', validator: (v) => ['left', 'right', 'auto'].includes(v) }, // force horizontal alignment
})

const emit = defineEmits(['update:show'])

// Reactive style — updated manually so we can react to DOM geometry changes
// (scroll, resize) that Vue's computed cannot track.
const menuStyle = ref({})

/** Recalculate position from current anchor geometry. */
function updatePosition() {
  if (!props.targetElement) { menuStyle.value = {}; return }
  const rect = props.targetElement.getBoundingClientRect()
  menuStyle.value = computeMenuStyle(rect, {
    maxWidth: props.maxWidth,
    maxHeight: props.maxHeight,
    edgeMargin: props.edgeMargin,
    menuItemsCount: props.menuItemsCount,
    anchor: props.anchor,
  })
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

watch(() => props.show, (val) => {
  if (val) {
    // Compute position — defer one frame so that a soft keyboard dismissal
    // triggered by the same tap can begin before we read getBoundingClientRect().
    // The menu is inside a Transition so it won't paint until the next tick anyway.
    requestAnimationFrame(() => {
      if (!props.show) return // may have been closed already
      updatePosition()
    })
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
    // Use setTimeout to avoid the opening click being treated as outside click
    setTimeout(() => {
      if (props.show) {
        document.addEventListener('click', handleClickOutside)
      }
    }, 0)
  } else {
    window.removeEventListener('scroll', onLayoutChange, true)
    window.removeEventListener('resize', onLayoutChange)
    if (window.visualViewport) {
      window.visualViewport.removeEventListener('resize', onLayoutChange)
      window.visualViewport.removeEventListener('scroll', onLayoutChange)
    }
    document.removeEventListener('click', handleClickOutside)
  }
})

// Cleanup on unmount
onBeforeUnmount(() => {
  window.removeEventListener('scroll', onLayoutChange, true)
  window.removeEventListener('resize', onLayoutChange)
  if (window.visualViewport) {
    window.visualViewport.removeEventListener('resize', onLayoutChange)
    window.visualViewport.removeEventListener('scroll', onLayoutChange)
  }
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style scoped>
.popup-menu {
  background: var(--bg-secondary, #fff);
  border: 1px solid var(--border-color, #e5e5e5);
  border-radius: 8px;
  box-shadow: 0 -4px 12px rgba(0, 0, 0, 0.12);
  z-index: 9999;
  padding: 3px 0;
}

/* Fade animation for menu appearance */
.menu-fade-enter-active,
.menu-fade-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}

.menu-fade-enter-from,
.menu-fade-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
</style>
